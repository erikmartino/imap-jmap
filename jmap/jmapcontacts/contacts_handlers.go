package jmapcontacts

import (
	"context"
	"encoding/json"
	"mime"
	"strings"
	"unicode/utf8"

	"github.com/vincent-petithory/dataurl"

	"imap-jmap/jmap/jmapauth"
	"imap-jmap/jmap/jmapblob"
	"imap-jmap/jmap/jmapcopy"
	"imap-jmap/jmap/jmapcore"
	"imap-jmap/jmap/jmaphandler"
)

func isOwnerPrincipal(princID string, accountID string, ownerUser string) bool {
	if princID == "" {
		return false
	}
	if princID == "primary" || princID == accountID || princID == ownerUser {
		return true
	}
	if ownerUser != "" && (princID == jmapauth.AccountIDForSubject(ownerUser) || jmapauth.AccountIDForSubject(princID) == jmapauth.AccountIDForSubject(ownerUser)) {
		return true
	}
	if accountID != "" && jmapauth.AccountIDForSubject(princID) == accountID {
		return true
	}
	return false
}

// validateAddressBookName enforces RFC 9610 Section 2: the name MUST NOT be the
// empty string and MUST NOT be greater than 255 octets when encoded as UTF-8.
func validateAddressBookName(name string) *jmapcore.SetError {
	if name == "" {
		return &jmapcore.SetError{Type: "invalidProperties", Description: "name must not be empty", Properties: []string{"name"}}
	}
	if len(name) > 255 {
		return &jmapcore.SetError{Type: "invalidProperties", Description: "name must not exceed 255 octets", Properties: []string{"name"}}
	}
	return nil
}

func validateUTF8(val any) bool {
	switch v := val.(type) {
	case string:
		return utf8.ValidString(v)
	case map[string]any:
		for k, item := range v {
			if !utf8.ValidString(k) || !validateUTF8(item) {
				return false
			}
		}
	case []any:
		for _, item := range v {
			if !validateUTF8(item) {
				return false
			}
		}
	}
	return true
}

// validateCardInput enforces RFC 9610 Section 3 create constraints: every value
// in addressBookIds MUST be true, no two ContactCards in an account may
// share a uid, photo files must be a recognised image type (RFC 9610 §3.5),
// and strings must be valid UTF-8 (RFC 9610 §5).
func validateCardInput(ctx context.Context, cardMap map[string]any, existingUIDs map[string]bool, accountID string, backend ContactsBackend, blobBackend jmapblob.BlobBackend) *jmapcore.SetError {
	if !validateUTF8(cardMap) {
		return &jmapcore.SetError{Type: "invalidProperties", Description: "string contains invalid UTF-8"}
	}
	if abRaw, ok := cardMap["addressBookIds"]; ok && abRaw != nil {
		abMap, ok := abRaw.(map[string]any)
		if !ok || len(abMap) == 0 {
			return &jmapcore.SetError{Type: "invalidProperties", Properties: []string{"addressBookIds"}}
		}
		var targetABIDs []jmapcore.Id
		for k, v := range abMap {
			if b, ok := v.(bool); !ok || !b {
				return &jmapcore.SetError{Type: "invalidProperties", Properties: []string{"addressBookIds"}}
			}
			targetABIDs = append(targetABIDs, jmapcore.Id(k))
		}
		if backend != nil && len(targetABIDs) > 0 {
			abs, _, err := backend.GetAddressBooks(ctx, targetABIDs)
			if err == nil {
				for _, ab := range abs {
					if ab != nil && !ab.MyRights.MayWrite {
						return &jmapcore.SetError{Type: "forbidden", Description: "mayWrite right is required on target addressbook"}
					}
				}
			}
		}
	}
	if uid, _ := cardMap["uid"].(string); uid != "" && existingUIDs[uid] {
		return &jmapcore.SetError{Type: "invalidProperties", Description: "a ContactCard with this uid already exists", Properties: []string{"uid"}}
	}
	// Validate media / photos per RFC 9610 §3.5
	if mediaRaw, ok := cardMap["media"].(map[string]any); ok {
		for _, mItem := range mediaRaw {
			mMap, ok := mItem.(map[string]any)
			if !ok {
				continue
			}
			kind, _ := mMap["kind"].(string)
			isPhoto := strings.EqualFold(kind, "photo") || kind == ""
			if isPhoto {
				if blobID, _ := mMap["blobId"].(string); blobID != "" {
					if blobBackend != nil {
						blob, found, err := blobBackend.GetBlob(ctx, accountID, blobID)
						if err != nil || !found {
							return &jmapcore.SetError{Type: "invalidProperties", Description: "blob not found for photo", Properties: []string{"media"}}
						}
						mediaType, _, err := mime.ParseMediaType(blob.Type)
						if err != nil || !strings.HasPrefix(mediaType, "image/") {
							return &jmapcore.SetError{Type: "invalidProperties", Description: "photo must be a recognised image type", Properties: []string{"media"}}
						}
					}
				}
				if uri, _ := mMap["uri"].(string); uri != "" {
					if strings.HasPrefix(uri, "data:") {
						du, err := dataurl.DecodeString(uri)
						if err != nil {
							return &jmapcore.SetError{Type: "invalidProperties", Description: "invalid data URI in photo", Properties: []string{"media"}}
						}
						mediaType, _, err := mime.ParseMediaType(du.ContentType())
						if err != nil || !strings.HasPrefix(mediaType, "image/") {
							return &jmapcore.SetError{Type: "invalidProperties", Description: "photo must be a recognised image type", Properties: []string{"media"}}
						}
					}
				}
				if mt, _ := mMap["mediaType"].(string); mt != "" {
					mediaType, _, err := mime.ParseMediaType(mt)
					if err != nil || !strings.HasPrefix(mediaType, "image/") {
						return &jmapcore.SetError{Type: "invalidProperties", Description: "photo must be a recognised image type", Properties: []string{"media"}}
					}
				}
			}
		}
	}
	return nil
}

// RegisterContactsHandlers registers RFC 9610 JMAP for Contacts method handlers into MethodRegistry.
func RegisterContactsHandlers(r *jmaphandler.MethodRegistry, backend ContactsBackend, blobBackend ...jmapblob.BlobBackend) {
	if backend == nil {
		return
	}
	var bb jmapblob.BlobBackend
	if len(blobBackend) > 0 {
		bb = blobBackend[0]
	}
	r.Register("AddressBook/get", handleAddressBookGet(backend))
	r.Register("AddressBook/changes", handleAddressBookChanges(backend))
	r.Register("AddressBook/set", handleAddressBookSet(backend))
	r.Register("AddressBook/copy", handleAddressBookCopy(backend))

	// RFC 9610 names the object "ContactCard"; register those as the canonical methods.
	r.Register("ContactCard/get", aliasMethod("ContactCard/get", handleCardGet(backend, bb)))
	r.Register("ContactCard/changes", aliasMethod("ContactCard/changes", handleCardChanges(backend)))
	r.Register("ContactCard/set", aliasMethod("ContactCard/set", handleCardSet(backend, bb)))
	r.Register("ContactCard/query", aliasMethod("ContactCard/query", handleCardQuery(backend)))
	r.Register("ContactCard/queryChanges", aliasMethod("ContactCard/queryChanges", handleCardQueryChanges(backend)))
	r.Register("ContactCard/copy", aliasMethod("ContactCard/copy", handleCardCopy(backend)))

	// "Card/*" retained as aliases for backward compatibility with existing clients/tests.
	r.Register("Card/get", handleCardGet(backend, bb))
	r.Register("Card/changes", handleCardChanges(backend))
	r.Register("Card/set", handleCardSet(backend, bb))
	r.Register("Card/query", handleCardQuery(backend))
	r.Register("Card/queryChanges", handleCardQueryChanges(backend))
	r.Register("Card/copy", handleCardCopy(backend))
}

func handleAddressBookGet(backend ContactsBackend) jmaphandler.MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		idsRaw, hasIDs := args["ids"].([]any)
		props := parseProperties(args)

		var list []*AddressBook
		var notFound []jmapcore.Id
		var err error

		if hasIDs {
			ids := make([]jmapcore.Id, 0, len(idsRaw))
			for _, item := range idsRaw {
				if idStr, ok := item.(string); ok {
					ids = append(ids, jmapcore.Id(idStr))
				}
			}
			list, notFound, err = backend.GetAddressBooks(ctx, ids)
		} else {
			list, err = backend.GetAllAddressBooks(ctx)
		}

		if err != nil || list == nil {
			list = []*AddressBook{}
		}
		if notFound == nil {
			notFound = []jmapcore.Id{}
		}

		return "AddressBook/get", map[string]any{
			"accountId": accountID,
			"state":     backend.AddressBookState(ctx),
			"list":      filterList(list, props),
			"notFound":  notFound,
		}
	}
}

func handleAddressBookChanges(backend ContactsBackend) jmaphandler.MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		sinceState, _ := args["sinceState"].(string)
		created, updated, destroyed, newState, hasMore := backend.AddressBookChanges(ctx, sinceState)
		if created == nil {
			created = []jmapcore.Id{}
		}
		if updated == nil {
			updated = []jmapcore.Id{}
		}
		if destroyed == nil {
			destroyed = []jmapcore.Id{}
		}
		return "AddressBook/changes", map[string]any{
			"accountId":      accountID,
			"oldState":       sinceState,
			"newState":       newState,
			"hasMoreChanges": hasMore,
			"created":        created,
			"updated":        updated,
			"destroyed":      destroyed,
		}
	}
}

func handleAddressBookSet(backend ContactsBackend) jmaphandler.MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		oldState := backend.AddressBookState(ctx)

		if ifInState, ok := args["ifInState"].(string); ok && ifInState != "" && ifInState != oldState {
			return "error", MethodErrorArgs("stateMismatch", "state mismatch")
		}
		created := make(map[string]*AddressBook)
		updated := make(map[string]map[string]any)
		destroyed := make([]jmapcore.Id, 0)
		notCreated := make(map[string]any)
		notUpdated := make(map[string]any)
		notDestroyed := make(map[string]any)
		creationRefs := newSetCreationRefs(ctx)

		if createRaw, ok := args["create"].(map[string]any); ok {
			for creationID, itemRaw := range createRaw {
				abMap, _ := itemRaw.(map[string]any)
				if _, hasIsDefault := abMap["isDefault"]; hasIsDefault {
					notCreated[creationID] = jmapcore.SetError{
						Type:        "invalidProperties",
						Description: "isDefault is server-set and cannot be set directly",
						Properties:  []string{"isDefault"},
					}
					continue
				}
				abBytes, _ := json.Marshal(abMap)
				var ab AddressBook
				_ = json.Unmarshal(abBytes, &ab)
				if se := validateAddressBookName(ab.Name); se != nil {
					notCreated[creationID] = *se
					continue
				}
				// RFC 9610 Section 2: sortOrder MUST be in the range 0 <= sortOrder < 2^31.
				if ab.SortOrder >= 1<<31 {
					notCreated[creationID] = jmapcore.SetError{Type: "invalidProperties", Description: "sortOrder out of range", Properties: []string{"sortOrder"}}
					continue
				}

				if ab.ShareWith != nil {
					ownerUser, _ := jmapauth.SubjectFromContext(ctx)
					hasOwner := false
					for princID := range ab.ShareWith {
						if isOwnerPrincipal(princID, accountID, ownerUser) {
							hasOwner = true
							break
						}
					}
					if hasOwner {
						notCreated[creationID] = jmapcore.SetError{
							Type:        "invalidProperties",
							Description: "the Principal to which this AddressBook belongs MUST NOT be in shareWith",
							Properties:  []string{"shareWith"},
						}
						continue
					}
				}

				createdAB, err := backend.CreateAddressBook(ctx, &ab)
				if err != nil {
					notCreated[creationID] = jmapcore.SetError{Type: "invalidProperties", Description: err.Error()}
				} else {
					created[creationID] = createdAB
					recordCreationRefs(ctx, creationRefs, creationID, createdAB.ID)
				}
			}
		}

		if updateRaw, ok := args["update"].(map[string]any); ok {
			for idStr, patchRaw := range updateRaw {
				resolvedID := resolveCreationID(idStr, creationRefs)
				if patch, ok := patchRaw.(map[string]any); ok {
					if _, hasIsDefault := patch["isDefault"]; hasIsDefault {
						notUpdated[string(resolvedID)] = jmapcore.SetError{
							Type:        "invalidProperties",
							Description: "isDefault is server-set and cannot be set directly",
							Properties:  []string{"isDefault"},
						}
						continue
					}
					if nameRaw, hasName := patch["name"]; hasName {
						name, _ := nameRaw.(string)
						if se := validateAddressBookName(name); se != nil {
							notUpdated[string(resolvedID)] = *se
							continue
						}
					}

					existingList, _, _ := backend.GetAddressBooks(ctx, []jmapcore.Id{jmapcore.Id(resolvedID)})
					var existingAB *AddressBook
					if len(existingList) > 0 {
						existingAB = existingList[0]
					}

					if shareWithRaw, hasShareWith := patch["shareWith"]; hasShareWith {
						if existingAB != nil && !existingAB.MyRights.MayShare {
							notUpdated[string(resolvedID)] = jmapcore.SetError{
								Type:        "forbidden",
								Description: "mayShare right is required to modify shareWith",
							}
							continue
						}
						if shareWithMap, ok := shareWithRaw.(map[string]any); ok && shareWithMap != nil {
							ownerUser, _ := jmapauth.SubjectFromContext(ctx)
							hasOwner := false
							for princID := range shareWithMap {
								if isOwnerPrincipal(princID, accountID, ownerUser) {
									hasOwner = true
									break
								}
							}
							if hasOwner {
								notUpdated[string(resolvedID)] = jmapcore.SetError{
									Type:        "invalidProperties",
									Description: "the Principal to which this AddressBook belongs MUST NOT be in shareWith",
									Properties:  []string{"shareWith"},
								}
								continue
							}

							// Right escalation check (RFC 9610 §2.3)
							if existingAB != nil {
								escalation := false
								for princID, rRaw := range shareWithMap {
									rMap, _ := rRaw.(map[string]any)
									if rMap == nil {
										continue
									}
									var prevRights *AddressBookRights
									if existingAB.ShareWith != nil {
										prevRights = existingAB.ShareWith[princID]
									}
									checkRight := func(rName string, myRight bool, prevGetter func(*AddressBookRights) bool) bool {
										if v, ok := rMap[rName].(bool); ok && v {
											prev := false
											if prevRights != nil {
												prev = prevGetter(prevRights)
											}
											if !prev && !myRight {
												return true
											}
										}
										return false
									}
									if checkRight("mayRead", existingAB.MyRights.MayRead, func(r *AddressBookRights) bool { return r.MayRead }) ||
										checkRight("mayWrite", existingAB.MyRights.MayWrite, func(r *AddressBookRights) bool { return r.MayWrite }) ||
										checkRight("mayShare", existingAB.MyRights.MayShare, func(r *AddressBookRights) bool { return r.MayShare }) ||
										checkRight("mayDelete", existingAB.MyRights.MayDelete, func(r *AddressBookRights) bool { return r.MayDelete }) {
										escalation = true
										break
									}
								}
								if escalation {
									notUpdated[string(resolvedID)] = jmapcore.SetError{
										Type:        "forbidden",
										Description: "cannot grant rights not possessed by current user",
									}
									continue
								}
							}
						}
					}

					updatedAB, err := backend.UpdateAddressBook(ctx, jmapcore.Id(resolvedID), resolvePatchCreationRefs(patch, creationRefs))
					if err != nil {
						if strings.HasPrefix(err.Error(), "forbidden:") {
							notUpdated[string(resolvedID)] = jmapcore.SetError{Type: "forbidden", Description: strings.TrimPrefix(err.Error(), "forbidden: ")}
						} else {
							notUpdated[string(resolvedID)] = jmapcore.SetError{Type: "notFound", Description: err.Error()}
						}
					} else {
						updated[string(resolvedID)] = map[string]any{"name": updatedAB.Name}
					}
				}
			}
		}

		if len(notCreated) == 0 && len(notUpdated) == 0 && len(notDestroyed) == 0 {
			if setDefaultRaw, ok := args["onSuccessSetIsDefault"].(string); ok && setDefaultRaw != "" {
				targetID := resolveCreationID(setDefaultRaw, creationRefs)
				if err := backend.SetDefaultAddressBook(ctx, jmapcore.Id(targetID)); err == nil {
					if cItem, ok := created[setDefaultRaw]; ok && cItem != nil {
						cItem.IsDefault = true
					} else {
						if _, ok := updated[string(targetID)]; !ok {
							updated[string(targetID)] = map[string]any{"isDefault": true}
						} else {
							updated[string(targetID)]["isDefault"] = true
						}
					}
				}
			}
		}

		onDestroyRemoveContents, _ := args["onDestroyRemoveContents"].(bool)
		if destroyRaw, ok := args["destroy"].([]any); ok {
			for _, idItem := range destroyRaw {
				if idStr, ok := idItem.(string); ok {
					resolvedID := resolveCreationID(idStr, creationRefs)
					existingList, _, _ := backend.GetAddressBooks(ctx, []jmapcore.Id{jmapcore.Id(resolvedID)})
					if len(existingList) > 0 && existingList[0] != nil {
						if !existingList[0].MyRights.MayDelete {
							notDestroyed[string(resolvedID)] = jmapcore.SetError{
								Type:        "forbidden",
								Description: "mayDelete right is required to delete addressbook",
							}
							continue
						}
					}
					if !onDestroyRemoveContents {
						hasContents, _ := backend.AddressBookHasContents(ctx, jmapcore.Id(resolvedID))
						if hasContents {
							notDestroyed[string(resolvedID)] = jmapcore.SetError{
								Type:        "addressBookHasContents",
								Description: "addressbook contains cards; use onDestroyRemoveContents to delete",
							}
							continue
						}
					}
					okDel, err := backend.DeleteAddressBook(ctx, jmapcore.Id(resolvedID), onDestroyRemoveContents)
					if err != nil || !okDel {
						notDestroyed[string(resolvedID)] = jmapcore.SetError{Type: "notFound", Description: "addressbook not found"}
					} else {
						destroyed = append(destroyed, jmapcore.Id(resolvedID))
					}
				}
			}
		}

		return "AddressBook/set", map[string]any{
			"accountId":    accountID,
			"oldState":     oldState,
			"newState":     backend.AddressBookState(ctx),
			"created":      created,
			"updated":      updated,
			"destroyed":    destroyed,
			"notCreated":   notCreated,
			"notUpdated":   notUpdated,
			"notDestroyed": notDestroyed,
		}
	}
}

func normalizeCardName(card *Card) {
	if card == nil || card.Name == nil {
		return
	}
	if len(card.Name.Components) == 0 && card.Name.Full != "" {
		if strings.EqualFold(card.Kind, "group") {
			card.Name.Components = []*JSContactNameComponent{
				{Value: card.Name.Full, Kind: "given"},
			}
		} else {
			parts := strings.Fields(card.Name.Full)
			if len(parts) == 1 {
				card.Name.Components = []*JSContactNameComponent{
					{Value: parts[0], Kind: "given"},
				}
			} else if len(parts) >= 2 {
				card.Name.Components = []*JSContactNameComponent{
					{Value: parts[0], Kind: "given"},
					{Value: strings.Join(parts[1:], " "), Kind: "surname"},
				}
			}
		}
	} else if card.Name.Full == "" && len(card.Name.Components) > 0 {
		var compVals []string
		for _, c := range card.Name.Components {
			if c != nil && c.Value != "" {
				compVals = append(compVals, c.Value)
			}
		}
		card.Name.Full = strings.Join(compVals, " ")
	}
}

func handleCardGet(backend ContactsBackend, blobBackend jmapblob.BlobBackend) jmaphandler.MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		idsRaw, hasIDs := args["ids"].([]any)
		props := parseProperties(args)

		var list []*Card
		var notFound []jmapcore.Id
		var err error

		if hasIDs {
			ids := make([]jmapcore.Id, 0, len(idsRaw))
			for _, item := range idsRaw {
				if idStr, ok := item.(string); ok {
					ids = append(ids, jmapcore.Id(idStr))
				}
			}
			list, notFound, err = backend.GetCards(ctx, ids)
		} else {
			list, err = backend.GetAllCards(ctx)
		}

		if err != nil || list == nil {
			list = []*Card{}
		}

		allABs, _ := backend.GetAllAddressBooks(ctx)
		abRights := make(map[jmapcore.Id]bool)
		for _, ab := range allABs {
			if ab != nil {
				abRights[ab.ID] = ab.MyRights.MayRead
			}
		}

		var filtered []*Card
		for _, card := range list {
			if card == nil {
				continue
			}
			if len(card.AddressBookIDs) > 0 && len(abRights) > 0 {
				canRead := false
				for abID := range card.AddressBookIDs {
					if mayRead, ok := abRights[abID]; ok && mayRead {
						canRead = true
						break
					}
				}
				if !canRead {
					continue
				}
			}
			if card.Type == "" {
				card.Type = "Card"
			}
			if card.Version == "" {
				card.Version = "1.0"
			}
			normalizeCardName(card)

			if card.Media != nil {
				for _, m := range card.Media {
					if m == nil {
						continue
					}
					if strings.HasPrefix(m.URI, "data:") {
						du, err := dataurl.DecodeString(m.URI)
						if err == nil {
							if blobBackend != nil {
								if uploaded, putErr := blobBackend.PutBlob(ctx, accountID, du.ContentType(), du.Data); putErr == nil && uploaded != nil {
									m.BlobID = jmapcore.Id(uploaded.ID)
								}
							}
							m.MediaType = du.ContentType()
							m.URI = ""
						}
					} else if m.BlobID != "" {
						if m.MediaType == "" && blobBackend != nil {
							if blob, ok, _ := blobBackend.GetBlob(ctx, accountID, string(m.BlobID)); ok && blob != nil {
								m.MediaType = blob.Type
							}
						}
						m.URI = ""
					}
				}
			}

			filtered = append(filtered, card)
		}
		if notFound == nil {
			notFound = []jmapcore.Id{}
		}

		return "Card/get", map[string]any{
			"accountId": accountID,
			"state":     backend.CardState(ctx),
			"list":      filterList(filtered, props),
			"notFound":  notFound,
		}
	}
}

func handleCardChanges(backend ContactsBackend) jmaphandler.MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		sinceState, _ := args["sinceState"].(string)
		created, updated, destroyed, newState, hasMore := backend.CardChanges(ctx, sinceState)
		if created == nil {
			created = []jmapcore.Id{}
		}
		if updated == nil {
			updated = []jmapcore.Id{}
		}
		if destroyed == nil {
			destroyed = []jmapcore.Id{}
		}
		return "Card/changes", map[string]any{
			"accountId":      accountID,
			"oldState":       sinceState,
			"newState":       newState,
			"hasMoreChanges": hasMore,
			"created":        created,
			"updated":        updated,
			"destroyed":      destroyed,
		}
	}
}

func handleCardSet(backend ContactsBackend, blobBackend jmapblob.BlobBackend) jmaphandler.MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		oldState := backend.CardState(ctx)

		if ifInState, ok := args["ifInState"].(string); ok && ifInState != "" && ifInState != oldState {
			return "error", MethodErrorArgs("stateMismatch", "state mismatch")
		}
		created := make(map[string]*Card)
		updated := make(map[string]map[string]any)
		destroyed := make([]jmapcore.Id, 0)
		notCreated := make(map[string]any)
		notUpdated := make(map[string]any)
		notDestroyed := make(map[string]any)

		// creationRefs maps a creation id to the real id the server assigned (seeded from
		// the request-scoped createdIds map), so #creationId references in this call and
		// in later method calls of the same request resolve (RFC 8620 Section 5.3).
		creationRefs := newSetCreationRefs(ctx)

		// uids already in use, so a batch cannot create two cards with the same
		// uid (RFC 9610 Section 3: at most one ContactCard per uid).
		existingUIDs := make(map[string]bool)
		if cards, err := backend.GetAllCards(ctx); err == nil {
			for _, c := range cards {
				if c != nil && c.Uid != "" {
					existingUIDs[c.Uid] = true
				}
			}
		}

		if createRaw, ok := args["create"].(map[string]any); ok {
			notCreated = runCreateLoop(createRaw, creationRefs, func(creationID string, resolvedMap map[string]any) (string, error) {
				if se := validateCardInput(ctx, resolvedMap, existingUIDs, accountID, backend, blobBackend); se != nil {
					return "", se
				}
				cardBytes, _ := json.Marshal(resolvedMap)
				var card Card
				_ = json.Unmarshal(cardBytes, &card)
				if card.Type == "" {
					card.Type = "Card"
				}
				if card.Version == "" {
					card.Version = "1.0"
				}
				normalizeCardName(&card)
				if card.Uid != "" {
					existingUIDs[card.Uid] = true
				}

				if card.Media != nil {
					for _, m := range card.Media {
						if m == nil {
							continue
						}
						if strings.HasPrefix(m.URI, "data:") {
							du, err := dataurl.DecodeString(m.URI)
							if err == nil {
								if blobBackend != nil {
									if uploaded, putErr := blobBackend.PutBlob(ctx, accountID, du.ContentType(), du.Data); putErr == nil && uploaded != nil {
										m.BlobID = jmapcore.Id(uploaded.ID)
									}
								}
								m.MediaType = du.ContentType()
								m.URI = ""
							}
						} else if m.BlobID != "" {
							if m.MediaType == "" && blobBackend != nil {
								if blob, ok, _ := blobBackend.GetBlob(ctx, accountID, string(m.BlobID)); ok && blob != nil {
									m.MediaType = blob.Type
								}
							}
							m.URI = ""
						}
					}
				}

				createdCard, err := backend.CreateCard(ctx, &card)
				if err != nil {
					return "", err
				}
				created[creationID] = createdCard
				recordCreationRefs(ctx, creationRefs, creationID, createdCard.ID)
				return string(createdCard.ID), nil
			})
		}

		if updateRaw, ok := args["update"].(map[string]any); ok {
			for idStr, patchRaw := range updateRaw {
				if rawPatch, ok := patchRaw.(map[string]any); ok {
					patch := resolvePatchCreationRefs(rawPatch, creationRefs)
					resolvedID := resolveCreationID(idStr, creationRefs)

					if !validateUTF8(patch) {
						notUpdated[string(resolvedID)] = jmapcore.SetError{Type: "invalidProperties", Description: "string contains invalid UTF-8"}
						continue
					}

					existingCards, _, _ := backend.GetCards(ctx, []jmapcore.Id{jmapcore.Id(resolvedID)})
					if len(existingCards) > 0 && existingCards[0] != nil {
						existingCard := existingCards[0]
						var abIDs []jmapcore.Id
						for id := range existingCard.AddressBookIDs {
							abIDs = append(abIDs, id)
						}
						abs, _, _ := backend.GetAddressBooks(ctx, abIDs)
						forbidden := false
						for _, ab := range abs {
							if ab != nil && !ab.MyRights.MayWrite {
								notUpdated[string(resolvedID)] = jmapcore.SetError{Type: "forbidden", Description: "mayWrite right is required on card's addressbook"}
								forbidden = true
								break
							}
						}
						if forbidden {
							continue
						}
					}

					if abRaw, ok := patch["addressBookIds"].(map[string]any); ok {
						var targetABIDs []jmapcore.Id
						for k := range abRaw {
							targetABIDs = append(targetABIDs, jmapcore.Id(k))
						}
						if len(targetABIDs) > 0 {
							abs, _, _ := backend.GetAddressBooks(ctx, targetABIDs)
							forbidden := false
							for _, ab := range abs {
								if ab != nil && !ab.MyRights.MayWrite {
									notUpdated[string(resolvedID)] = jmapcore.SetError{Type: "forbidden", Description: "mayWrite right is required on target addressbook"}
									forbidden = true
									break
								}
							}
							if forbidden {
								continue
							}
						}
					}

					if mediaRaw, ok := patch["media"].(map[string]any); ok {
						var photoErr *jmapcore.SetError
						for _, mItem := range mediaRaw {
							mMap, ok := mItem.(map[string]any)
							if !ok {
								continue
							}
							kind, _ := mMap["kind"].(string)
							isPhoto := strings.EqualFold(kind, "photo") || kind == ""
							if isPhoto {
								if blobID, _ := mMap["blobId"].(string); blobID != "" && blobBackend != nil {
									blob, found, err := blobBackend.GetBlob(ctx, accountID, blobID)
									if err != nil || !found {
										photoErr = &jmapcore.SetError{Type: "invalidProperties", Description: "blob not found for photo", Properties: []string{"media"}}
										break
									}
									mediaType, _, err := mime.ParseMediaType(blob.Type)
									if err != nil || !strings.HasPrefix(mediaType, "image/") {
										photoErr = &jmapcore.SetError{Type: "invalidProperties", Description: "photo must be a recognised image type", Properties: []string{"media"}}
										break
									}
								}
								if uri, _ := mMap["uri"].(string); uri != "" && strings.HasPrefix(uri, "data:") {
									du, err := dataurl.DecodeString(uri)
									if err != nil {
										photoErr = &jmapcore.SetError{Type: "invalidProperties", Description: "invalid data URI in photo", Properties: []string{"media"}}
										break
									}
									mediaType, _, err := mime.ParseMediaType(du.ContentType())
									if err != nil || !strings.HasPrefix(mediaType, "image/") {
										photoErr = &jmapcore.SetError{Type: "invalidProperties", Description: "photo must be a recognised image type", Properties: []string{"media"}}
										break
									}
								}
							}
						}
						if photoErr != nil {
							notUpdated[string(resolvedID)] = *photoErr
							continue
						}
					}

					updatedCard, err := backend.UpdateCard(ctx, jmapcore.Id(resolvedID), patch)
					if err != nil {
						notUpdated[string(resolvedID)] = jmapcore.SetError{Type: "notFound", Description: err.Error()}
					} else {
						updated[string(resolvedID)] = map[string]any{"updated": updatedCard.Updated}
					}
				}
			}
		}

		if destroyRaw, ok := args["destroy"].([]any); ok {
			for _, idItem := range destroyRaw {
				if idStr, ok := idItem.(string); ok {
					resolvedID := resolveCreationID(idStr, creationRefs)
					existingCards, _, _ := backend.GetCards(ctx, []jmapcore.Id{jmapcore.Id(resolvedID)})
					if len(existingCards) > 0 && existingCards[0] != nil {
						var abIDs []jmapcore.Id
						for id := range existingCards[0].AddressBookIDs {
							abIDs = append(abIDs, id)
						}
						abs, _, _ := backend.GetAddressBooks(ctx, abIDs)
						forbidden := false
						for _, ab := range abs {
							if ab != nil && !ab.MyRights.MayWrite {
								notDestroyed[string(resolvedID)] = jmapcore.SetError{Type: "forbidden", Description: "mayWrite right is required on card's addressbook"}
								forbidden = true
								break
							}
						}
						if forbidden {
							continue
						}
					}
					ok, err := backend.DeleteCard(ctx, jmapcore.Id(resolvedID))
					if err != nil || !ok {
						notDestroyed[string(resolvedID)] = jmapcore.SetError{Type: "notFound", Description: "card not found"}
					} else {
						destroyed = append(destroyed, jmapcore.Id(resolvedID))
					}
				}
			}
		}

		return "Card/set", map[string]any{
			"accountId":    accountID,
			"oldState":     oldState,
			"newState":     backend.CardState(ctx),
			"created":      created,
			"updated":      updated,
			"destroyed":    destroyed,
			"notCreated":   notCreated,
			"notUpdated":   notUpdated,
			"notDestroyed": notDestroyed,
		}
	}
}

var cardSortableProperties = map[string]bool{
	"created":       true,
	"updated":       true,
	"name/given":    true,
	"name/surname":  true,
	"name/surname2": true,
}

func handleCardQuery(backend ContactsBackend) jmaphandler.MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		filter, _ := args["filter"].(map[string]any)

		comparators := parseComparators(args)
		if errType, errMsg := validateComparators(comparators, cardSortableProperties); errType != "" {
			return "error", MethodErrorArgs(errType, errMsg)
		}

		position, posErr := parseQueryPosition(args)
		if posErr != "" {
			return "error", MethodErrorArgs(MethodErrorInvalidArguments, posErr)
		}

		anchor, anchorOffset, anchorErr := parseQueryAnchor(args)
		if anchorErr != "" {
			return "error", MethodErrorArgs(MethodErrorInvalidArguments, anchorErr)
		}

		var limit *uint64
		if lim, ok := args["limit"].(float64); ok {
			l := uint64(lim)
			limit = &l
		}

		var ids []jmapcore.Id
		var total int
		if anchor != "" {
			allIDs, allTotal, _ := backend.QueryCards(ctx, filter, comparators, 0, nil)
			total = allTotal
			var found bool
			position, ids, found = applyQueryAnchor(anchor, anchorOffset, allIDs, limit)
			if !found {
				return "error", MethodErrorArgs(MethodErrorAnchorNotFound, "anchor not found in results: "+anchor)
			}
		} else {
			ids, total, _ = backend.QueryCards(ctx, filter, comparators, position, limit)
		}
		if ids == nil {
			ids = []jmapcore.Id{}
		}
		position = NormalizePosition(position, total)

		state := backend.CardState(ctx)
		return "Card/query", map[string]any{
			"accountId":           accountID,
			"queryState":          state,
			"canCalculateChanges": true,
			"position":            position,
			"total":               total,
			"ids":                 ids,
		}
	}
}

func handleCardQueryChanges(backend ContactsBackend) jmaphandler.MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		upToID, _ := args["upToId"].(string)
		sinceState, _ := args["sinceQueryState"].(string)

		if sinceState == "" {
			return "error", MethodErrorArgs(MethodErrorInvalidArguments, "sinceQueryState is required")
		}

		createdIDs, updatedIDs, destroyedIDs, newState, hasMore := backend.CardChanges(ctx, sinceState)
		if hasMore {
			return "error", MethodErrorArgs("cannotCalculateChanges", "sinceQueryState is too old")
		}

		filter, _ := args["filter"].(map[string]any)
		comparators := parseComparators(args)
		if errType, errMsg := validateComparators(comparators, cardSortableProperties); errType != "" {
			return "error", MethodErrorArgs(errType, errMsg)
		}
		currentIDs, _, _ := backend.QueryCards(ctx, filter, comparators, 0, nil)
		added, removed := computeQueryChanges(createdIDs, updatedIDs, destroyedIDs, currentIDs, upToID)

		res := map[string]any{
			"accountId":     accountID,
			"oldQueryState": sinceState,
			"newQueryState": newState,
			"added":         added,
			"removed":       removed,
		}
		if upToID != "" {
			res["upToId"] = upToID
		}
		return "Card/queryChanges", res
	}
}

// handleCardCopy implements Card/copy per RFC 8620 Section 5.4: each create entry names a source
// card by id, optionally overriding properties, and is recreated in the target account.
func handleCardCopy(backend ContactsBackend) jmaphandler.MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, fromAccountID := jmapcopy.ResolveCopyAccountIDs(args)
		srcCtx := SourceAccountContext(ctx, args)

		oldState, errInv := jmapcopy.ValidateCopyStates(ctx, srcCtx, args, backend.CardState, backend.CardState)
		if errInv != nil {
			return errInv.Name, errInv.Args
		}

		onSuccessDestroyOriginal, _ := args["onSuccessDestroyOriginal"].(bool)
		created := make(map[string]*Card)
		notCreated := make(map[string]any)
		destroyOriginals := make([]jmapcore.Id, 0)
		creationRefs := newSetCreationRefs(ctx)

		if createRaw, ok := args["create"].(map[string]any); ok {
			for creationID, raw := range createRaw {
				m, _ := raw.(map[string]any)
				srcID, _ := m["id"].(string)
				if srcID == "" {
					notCreated[creationID] = jmapcore.SetError{Type: "invalidProperties", Description: "copy create entry must reference a source id"}
					continue
				}
				resolvedSrcID := resolveCreationID(srcID, creationRefs)
				srcs, notFound, _ := backend.GetCards(srcCtx, []jmapcore.Id{jmapcore.Id(resolvedSrcID)})
				if len(srcs) == 0 || len(notFound) > 0 {
					notCreated[creationID] = jmapcore.SetError{Type: "notFound", Description: "source card not found: " + srcID}
					continue
				}

				merged := mergeCopyOverrides(srcs[0], m)
				cardBytes, _ := json.Marshal(merged)
				var card Card
				_ = json.Unmarshal(cardBytes, &card)
				card.ID = ""

				newCard, err := backend.CreateCard(ctx, &card)
				if err != nil {
					notCreated[creationID] = jmapcore.SetError{Type: "invalidProperties", Description: err.Error()}
				} else {
					created[creationID] = newCard
					recordCreationRefs(ctx, creationRefs, creationID, newCard.ID)
					destroyOriginals = append(destroyOriginals, jmapcore.Id(resolvedSrcID))
				}
			}
		}

		if onSuccessDestroyOriginal {
			for _, srcID := range destroyOriginals {
				_, _ = backend.DeleteCard(srcCtx, srcID)
			}
		}

		return "Card/copy", map[string]any{
			"fromAccountId": fromAccountID,
			"accountId":     accountID,
			"oldState":      oldState,
			"newState":      backend.CardState(ctx),
			"created":       nilIfEmpty(created),
			"notCreated":    nilIfEmpty(notCreated),
		}
	}
}

// handleAddressBookCopy implements AddressBook/copy as a server extension per RFC 8620 Section 5.4.
func handleAddressBookCopy(backend ContactsBackend) jmaphandler.MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, fromAccountID := jmapcopy.ResolveCopyAccountIDs(args)
		srcCtx := SourceAccountContext(ctx, args)

		oldState, errInv := jmapcopy.ValidateCopyStates(ctx, srcCtx, args, backend.AddressBookState, backend.AddressBookState)
		if errInv != nil {
			return errInv.Name, errInv.Args
		}

		onSuccessDestroyOriginal, _ := args["onSuccessDestroyOriginal"].(bool)
		created := make(map[string]*AddressBook)
		notCreated := make(map[string]any)
		destroyOriginals := make([]jmapcore.Id, 0)
		creationRefs := newSetCreationRefs(ctx)

		if createRaw, ok := args["create"].(map[string]any); ok {
			for creationID, raw := range createRaw {
				m, _ := raw.(map[string]any)
				srcID, _ := m["id"].(string)
				if srcID == "" {
					notCreated[creationID] = jmapcore.SetError{Type: "invalidProperties", Description: "copy create entry must reference a source id"}
					continue
				}
				resolvedSrcID := resolveCreationID(srcID, creationRefs)
				srcs, notFound, _ := backend.GetAddressBooks(srcCtx, []jmapcore.Id{jmapcore.Id(resolvedSrcID)})
				if len(srcs) == 0 || len(notFound) > 0 {
					notCreated[creationID] = jmapcore.SetError{Type: "notFound", Description: "source address book not found: " + srcID}
					continue
				}

				merged := mergeCopyOverrides(srcs[0], m)
				abBytes, _ := json.Marshal(merged)
				var ab AddressBook
				_ = json.Unmarshal(abBytes, &ab)
				ab.ID = ""

				newAB, err := backend.CreateAddressBook(ctx, &ab)
				if err != nil {
					notCreated[creationID] = jmapcore.SetError{Type: "invalidProperties", Description: err.Error()}
				} else {
					created[creationID] = newAB
					recordCreationRefs(ctx, creationRefs, creationID, newAB.ID)
					destroyOriginals = append(destroyOriginals, jmapcore.Id(resolvedSrcID))
				}
			}
		}

		if onSuccessDestroyOriginal {
			for _, srcID := range destroyOriginals {
				_, _ = backend.DeleteAddressBook(srcCtx, srcID, false)
			}
		}

		return "AddressBook/copy", map[string]any{
			"fromAccountId": fromAccountID,
			"accountId":     accountID,
			"oldState":      oldState,
			"newState":      backend.AddressBookState(ctx),
			"created":       nilIfEmpty(created),
			"notCreated":    nilIfEmpty(notCreated),
		}
	}
}
