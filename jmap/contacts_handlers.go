package jmap

import (
	"context"
	"encoding/json"
)

// RegisterContactsHandlers registers RFC 9610 JMAP for Contacts method handlers into MethodRegistry.
func RegisterContactsHandlers(r *MethodRegistry, backend ContactsBackend) {
	if backend == nil {
		return
	}
	r.Register("AddressBook/get", handleAddressBookGet(backend))
	r.Register("AddressBook/changes", handleAddressBookChanges(backend))
	r.Register("AddressBook/set", handleAddressBookSet(backend))
	r.Register("AddressBook/copy", handleAddressBookCopy(backend))

	// RFC 9610 names the object "ContactCard"; register those as the canonical methods.
	r.Register("ContactCard/get", aliasMethod("ContactCard/get", handleCardGet(backend)))
	r.Register("ContactCard/changes", aliasMethod("ContactCard/changes", handleCardChanges(backend)))
	r.Register("ContactCard/set", aliasMethod("ContactCard/set", handleCardSet(backend)))
	r.Register("ContactCard/query", aliasMethod("ContactCard/query", handleCardQuery(backend)))
	r.Register("ContactCard/copy", aliasMethod("ContactCard/copy", handleCardCopy(backend)))

	// "Card/*" retained as aliases for backward compatibility with existing clients/tests.
	r.Register("Card/get", handleCardGet(backend))
	r.Register("Card/changes", handleCardChanges(backend))
	r.Register("Card/set", handleCardSet(backend))
	r.Register("Card/query", handleCardQuery(backend))
	r.Register("Card/copy", handleCardCopy(backend))
}

func handleAddressBookGet(backend ContactsBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		idsRaw, hasIDs := args["ids"].([]any)

		var list []*AddressBook
		var notFound []Id
		var err error

		if hasIDs {
			ids := make([]Id, 0, len(idsRaw))
			for _, item := range idsRaw {
				if idStr, ok := item.(string); ok {
					ids = append(ids, Id(idStr))
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
			notFound = []Id{}
		}

		return "AddressBook/get", map[string]any{
			"accountId": accountID,
			"state":     backend.AddressBookState(ctx),
			"list":      list,
			"notFound":  notFound,
		}
	}
}

func handleAddressBookChanges(backend ContactsBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		sinceState, _ := args["sinceState"].(string)
		created, updated, destroyed, newState, hasMore := backend.AddressBookChanges(ctx, sinceState)
		if created == nil {
			created = []Id{}
		}
		if updated == nil {
			updated = []Id{}
		}
		if destroyed == nil {
			destroyed = []Id{}
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

func handleAddressBookSet(backend ContactsBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		oldState := backend.AddressBookState(ctx)
		created := make(map[string]*AddressBook)
		updated := make(map[string]map[string]any)
		destroyed := make([]Id, 0)
		notCreated := make(map[string]any)
		notUpdated := make(map[string]any)
		notDestroyed := make(map[string]any)

		if createRaw, ok := args["create"].(map[string]any); ok {
			for creationID, abMap := range createRaw {
				abBytes, _ := json.Marshal(abMap)
				var ab AddressBook
				_ = json.Unmarshal(abBytes, &ab)

				createdAB, err := backend.CreateAddressBook(ctx, &ab)
				if err != nil {
					notCreated[creationID] = SetError{Type: "invalidProperties", Description: err.Error()}
				} else {
					created[creationID] = createdAB
				}
			}
		}

		if updateRaw, ok := args["update"].(map[string]any); ok {
			for idStr, patchRaw := range updateRaw {
				if patch, ok := patchRaw.(map[string]any); ok {
					updatedAB, err := backend.UpdateAddressBook(ctx, Id(idStr), patch)
					if err != nil {
						notUpdated[idStr] = SetError{Type: "notFound", Description: err.Error()}
					} else {
						updated[idStr] = map[string]any{"name": updatedAB.Name}
					}
				}
			}
		}

		if destroyRaw, ok := args["destroy"].([]any); ok {
			for _, idItem := range destroyRaw {
				if idStr, ok := idItem.(string); ok {
					ok, err := backend.DeleteAddressBook(ctx, Id(idStr))
					if err != nil || !ok {
						notDestroyed[idStr] = SetError{Type: "notFound", Description: "addressbook not found"}
					} else {
						destroyed = append(destroyed, Id(idStr))
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

func handleCardGet(backend ContactsBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		idsRaw, hasIDs := args["ids"].([]any)

		var list []*Card
		var notFound []Id
		var err error

		if hasIDs {
			ids := make([]Id, 0, len(idsRaw))
			for _, item := range idsRaw {
				if idStr, ok := item.(string); ok {
					ids = append(ids, Id(idStr))
				}
			}
			list, notFound, err = backend.GetCards(ctx, ids)
		} else {
			list, err = backend.GetAllCards(ctx)
		}

		if err != nil || list == nil {
			list = []*Card{}
		}
		if notFound == nil {
			notFound = []Id{}
		}

		return "Card/get", map[string]any{
			"accountId": accountID,
			"state":     backend.CardState(ctx),
			"list":      list,
			"notFound":  notFound,
		}
	}
}

func handleCardChanges(backend ContactsBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		sinceState, _ := args["sinceState"].(string)
		created, updated, destroyed, newState, hasMore := backend.CardChanges(ctx, sinceState)
		if created == nil {
			created = []Id{}
		}
		if updated == nil {
			updated = []Id{}
		}
		if destroyed == nil {
			destroyed = []Id{}
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

func handleCardSet(backend ContactsBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		oldState := backend.CardState(ctx)
		created := make(map[string]*Card)
		updated := make(map[string]map[string]any)
		destroyed := make([]Id, 0)
		notCreated := make(map[string]any)
		notUpdated := make(map[string]any)
		notDestroyed := make(map[string]any)

		if createRaw, ok := args["create"].(map[string]any); ok {
			for creationID, cardMap := range createRaw {
				cardBytes, _ := json.Marshal(cardMap)
				var card Card
				_ = json.Unmarshal(cardBytes, &card)

				createdCard, err := backend.CreateCard(ctx, &card)
				if err != nil {
					notCreated[creationID] = SetError{Type: "invalidProperties", Description: err.Error()}
				} else {
					created[creationID] = createdCard
				}
			}
		}

		if updateRaw, ok := args["update"].(map[string]any); ok {
			for idStr, patchRaw := range updateRaw {
				if patch, ok := patchRaw.(map[string]any); ok {
					updatedCard, err := backend.UpdateCard(ctx, Id(idStr), patch)
					if err != nil {
						notUpdated[idStr] = SetError{Type: "notFound", Description: err.Error()}
					} else {
						updated[idStr] = map[string]any{"updated": updatedCard.Updated}
					}
				}
			}
		}

		if destroyRaw, ok := args["destroy"].([]any); ok {
			for _, idItem := range destroyRaw {
				if idStr, ok := idItem.(string); ok {
					ok, err := backend.DeleteCard(ctx, Id(idStr))
					if err != nil || !ok {
						notDestroyed[idStr] = SetError{Type: "notFound", Description: "card not found"}
					} else {
						destroyed = append(destroyed, Id(idStr))
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

func handleCardQuery(backend ContactsBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		filter, _ := args["filter"].(map[string]any)

		var position int
		if pos, ok := args["position"].(float64); ok {
			position = int(pos)
		}

		var limit *uint64
		if lim, ok := args["limit"].(float64); ok {
			l := uint64(lim)
			limit = &l
		}

		ids, total, err := backend.QueryCards(ctx, filter, position, limit)
		if err != nil || ids == nil {
			ids = []Id{}
		}

		return "Card/query", map[string]any{
			"accountId":           accountID,
			"queryState":          "0",
			"canCalculateChanges": false,
			"position":            position,
			"total":               total,
			"ids":                 ids,
		}
	}
}

// handleAddressBookCopy implements AddressBook/copy per RFC 8620 Section 5.4.
func handleAddressBookCopy(backend ContactsBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		fromAccountID, _ := args["fromAccountId"].(string)
		if fromAccountID == "" {
			fromAccountID = accountID
		}
		oldState := backend.AddressBookState(ctx)

		created := make(map[string]*AddressBook)
		notCreated := make(map[string]any)

		if createRaw, ok := args["create"].(map[string]any); ok {
			for creationID, raw := range createRaw {
				m, _ := raw.(map[string]any)
				srcID, _ := m["id"].(string)
				if srcID == "" {
					notCreated[creationID] = SetError{Type: "invalidProperties", Description: "copy create entry must reference a source id"}
					continue
				}
				srcs, notFound, _ := backend.GetAddressBooks(ctx, []Id{Id(srcID)})
				if len(srcs) == 0 || len(notFound) > 0 {
					notCreated[creationID] = SetError{Type: "notFound", Description: "source address book not found: " + srcID}
					continue
				}

				merged := mergeCopyOverrides(srcs[0], m)
				abBytes, _ := json.Marshal(merged)
				var ab AddressBook
				_ = json.Unmarshal(abBytes, &ab)
				ab.ID = ""

				newAB, err := backend.CreateAddressBook(ctx, &ab)
				if err != nil {
					notCreated[creationID] = SetError{Type: "invalidProperties", Description: err.Error()}
				} else {
					created[creationID] = newAB
				}
			}
		}

		return "AddressBook/copy", map[string]any{
			"fromAccountId": fromAccountID,
			"accountId":     accountID,
			"oldState":      oldState,
			"newState":      backend.AddressBookState(ctx),
			"created":       created,
			"notCreated":    notCreated,
		}
	}
}

// handleCardCopy implements Card/copy per RFC 8620 Section 5.4: each create entry names a source
// card by id, optionally overriding properties, and is recreated in the target account.
func handleCardCopy(backend ContactsBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		fromAccountID, _ := args["fromAccountId"].(string)
		if fromAccountID == "" {
			fromAccountID = accountID
		}
		oldState := backend.CardState(ctx)

		created := make(map[string]*Card)
		notCreated := make(map[string]any)

		if createRaw, ok := args["create"].(map[string]any); ok {
			for creationID, raw := range createRaw {
				m, _ := raw.(map[string]any)
				srcID, _ := m["id"].(string)
				if srcID == "" {
					notCreated[creationID] = SetError{Type: "invalidProperties", Description: "copy create entry must reference a source id"}
					continue
				}
				srcs, notFound, _ := backend.GetCards(ctx, []Id{Id(srcID)})
				if len(srcs) == 0 || len(notFound) > 0 {
					notCreated[creationID] = SetError{Type: "notFound", Description: "source card not found: " + srcID}
					continue
				}

				merged := mergeCopyOverrides(srcs[0], m)
				cardBytes, _ := json.Marshal(merged)
				var card Card
				_ = json.Unmarshal(cardBytes, &card)
				card.ID = ""

				newCard, err := backend.CreateCard(ctx, &card)
				if err != nil {
					notCreated[creationID] = SetError{Type: "invalidProperties", Description: err.Error()}
				} else {
					created[creationID] = newCard
				}
			}
		}

		return "Card/copy", map[string]any{
			"fromAccountId": fromAccountID,
			"accountId":     accountID,
			"oldState":      oldState,
			"newState":      backend.CardState(ctx),
			"created":       created,
			"notCreated":    notCreated,
		}
	}
}
