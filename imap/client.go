package imap

import (
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

// Client wraps an IMAP client connection without leaking go-imap types.
type Client struct {
	cli      *imapclient.Client
	username string
}

func (c *Client) logCmd(cmd string, attrs ...any) func(*error) {
	start := time.Now()
	return func(errPtr *error) {
		dur := time.Since(start)
		fields := []any{
			"user", c.username,
			"command", cmd,
			"duration_ms", dur.Milliseconds(),
		}
		fields = append(fields, attrs...)
		if errPtr != nil && *errPtr != nil {
			fields = append(fields, "error", *errPtr)
		}
		slog.Info("IMAP request", fields...)
	}
}

// Dial connects and authenticates to an IMAP server at the specified address using the given credentials.
func Dial(addr string, username, password string) (*Client, error) {
	var c *imapclient.Client
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
		port = "993"
	}

	if port == "993" {
		client, err := imapclient.DialTLS(addr, &imapclient.Options{
			TLSConfig: &tls.Config{InsecureSkipVerify: true, ServerName: host},
		})
		if err != nil {
			return nil, fmt.Errorf("failed to dial TLS IMAP server %s: %w", addr, err)
		}
		c = client
	} else {
		client, err := imapclient.DialStartTLS(addr, &imapclient.Options{
			TLSConfig: &tls.Config{InsecureSkipVerify: true, ServerName: host},
		})
		if err != nil {
			client, err = imapclient.DialInsecure(addr, &imapclient.Options{})
			if err != nil {
				return nil, fmt.Errorf("failed to connect to IMAP server %s: %w", addr, err)
			}
		}
		c = client
	}

	start := time.Now()
	err = c.Login(username, password).Wait()
	dur := time.Since(start)
	slog.Info("IMAP request",
		"user", username,
		"command", "LOGIN",
		"duration_ms", dur.Milliseconds(),
		"error", err,
	)
	if err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("IMAP login failed for user %s: %w", username, err)
	}

	return &Client{cli: c, username: username}, nil
}

// RawClient returns the underlying imapclient.Client for internal operations within the imap package.
func (c *Client) RawClient() *imapclient.Client {
	return c.cli
}

// Noop issues a NOOP command to keep the connection alive.
func (c *Client) Noop() (err error) {
	defer c.logCmd("NOOP")(&err)
	return c.cli.Noop().Wait()
}

// Close closes the underlying IMAP client connection.
func (c *Client) Close() error {
	if c.cli != nil {
		return c.cli.Close()
	}
	return nil
}

// UnilateralHandlers provides callbacks for IMAP server notifications.
type UnilateralHandlers struct {
	Mailbox func()
	Expunge func(seqNum uint32)
	Fetch   func()
}

// DialIdle connects to an IMAP server, authenticates, and sets up a unilateral handler for IDLE.
func DialIdle(addr, username, password string, handlers UnilateralHandlers) (*Client, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
		port = "993"
	}

	opts := &imapclient.Options{
		UnilateralDataHandler: &imapclient.UnilateralDataHandler{
			Mailbox: func(data *imapclient.UnilateralDataMailbox) {
				if handlers.Mailbox != nil {
					handlers.Mailbox()
				}
			},
			Expunge: func(seqNum uint32) {
				if handlers.Expunge != nil {
					handlers.Expunge(seqNum)
				}
			},
			Fetch: func(msg *imapclient.FetchMessageData) {
				if handlers.Fetch != nil {
					handlers.Fetch()
				}
			},
		},
	}

	var c *imapclient.Client
	if port == "993" {
		opts.TLSConfig = &tls.Config{InsecureSkipVerify: true, ServerName: host}
		client, err := imapclient.DialTLS(addr, opts)
		if err != nil {
			return nil, err
		}
		c = client
	} else {
		opts.TLSConfig = &tls.Config{InsecureSkipVerify: true, ServerName: host}
		client, err := imapclient.DialStartTLS(addr, opts)
		if err != nil {
			client, err = imapclient.DialInsecure(addr, &imapclient.Options{
				UnilateralDataHandler: opts.UnilateralDataHandler,
			})
			if err != nil {
				return nil, err
			}
		}
		c = client
	}

	start := time.Now()
	err = c.Login(username, password).Wait()
	dur := time.Since(start)
	slog.Info("IMAP request",
		"user", username,
		"command", "LOGIN",
		"idle", true,
		"duration_ms", dur.Milliseconds(),
		"error", err,
	)
	if err != nil {
		_ = c.Close()
		return nil, err
	}

	return &Client{cli: c, username: username}, nil
}

// IdleCmd encapsulates an active IDLE command.
type IdleCmd struct {
	cmd *imapclient.IdleCommand
}

// Idle enters the IMAP IDLE state.
func (c *Client) Idle() (res *IdleCmd, err error) {
	defer c.logCmd("IDLE")(&err)
	cmd, err := c.cli.Idle()
	if err != nil {
		return nil, err
	}
	return &IdleCmd{cmd: cmd}, nil
}

// MailboxInfo represents an IMAP mailbox / folder listing.
type MailboxInfo struct {
	Name        string
	Attrs       []string
	Delimiter   rune
	Messages    uint32
	Unseen      uint32
	UIDValidity uint32
	UIDNext     uint32
}

// ListFolders lists IMAP folders matching the given reference and pattern.
func (c *Client) ListFolders(ref, pattern string) (res []MailboxInfo, err error) {
	defer c.logCmd("LIST", "ref", ref, "pattern", pattern)(&err)
	listCmd := c.cli.List(ref, pattern, nil)
	mbs, err := listCmd.Collect()
	if err != nil {
		return nil, err
	}
	for _, m := range mbs {
		var attrs []string
		for _, a := range m.Attrs {
			attrs = append(attrs, string(a))
		}
		var numMsgs, numUnseen, uidValidity, uidNext uint32
		if statusCmd := c.cli.Status(m.Mailbox, &imap.StatusOptions{NumMessages: true, NumUnseen: true, UIDValidity: true, UIDNext: true}); statusCmd != nil {
			if st, err := statusCmd.Wait(); err == nil {
				if st.NumMessages != nil {
					numMsgs = *st.NumMessages
				}
				if st.NumUnseen != nil {
					numUnseen = *st.NumUnseen
				}
				if st.UIDValidity != 0 {
					uidValidity = st.UIDValidity
				}
				if st.UIDNext != 0 {
					uidNext = uint32(st.UIDNext)
				}
			}
		}
		res = append(res, MailboxInfo{
			Name:        m.Mailbox,
			Attrs:       attrs,
			Delimiter:   m.Delim,
			Messages:    numMsgs,
			Unseen:      numUnseen,
			UIDValidity: uidValidity,
			UIDNext:     uidNext,
		})
	}
	return res, nil
}

// Select selects an IMAP folder.
func (c *Client) Select(folder string) (err error) {
	defer c.logCmd("SELECT", "folder", folder)(&err)
	_, err = c.cli.Select(folder, nil).Wait()
	return err
}

// Close terminates the IDLE session.
func (ic *IdleCmd) Close() error {
	return ic.cmd.Close()
}

// Wait waits for the IDLE command to finish.
func (ic *IdleCmd) Wait() error {
	return ic.cmd.Wait()
}

// Create creates a new IMAP folder.
func (c *Client) Create(folder string) (err error) {
	defer c.logCmd("CREATE", "folder", folder)(&err)
	return c.cli.Create(folder, nil).Wait()
}

// Delete deletes an IMAP folder.
func (c *Client) Delete(folder string) (err error) {
	defer c.logCmd("DELETE", "folder", folder)(&err)
	return c.cli.Delete(folder).Wait()
}

// Rename renames an IMAP folder.
func (c *Client) Rename(oldFolder, newFolder string) (err error) {
	defer c.logCmd("RENAME", "from", oldFolder, "to", newFolder)(&err)
	return c.cli.Rename(oldFolder, newFolder, nil).Wait()
}

// Subscribe subscribes to an IMAP folder.
func (c *Client) Subscribe(folder string) (err error) {
	defer c.logCmd("SUBSCRIBE", "folder", folder)(&err)
	return c.cli.Subscribe(folder).Wait()
}

// Unsubscribe unsubscribes from an IMAP folder.
func (c *Client) Unsubscribe(folder string) (err error) {
	defer c.logCmd("UNSUBSCRIBE", "folder", folder)(&err)
	return c.cli.Unsubscribe(folder).Wait()
}

// Append appends a raw RFC 822 message to a folder with given flags.
func (c *Client) Append(folder string, rawMsg []byte, flags []string, t time.Time) (err error) {
	defer c.logCmd("APPEND", "folder", folder, "size", len(rawMsg))(&err)
	var imapFlags []imap.Flag
	for _, f := range flags {
		imapFlags = append(imapFlags, imap.Flag(f))
	}
	opts := &imap.AppendOptions{
		Flags: imapFlags,
		Time:  t,
	}
	appendCmd := c.cli.Append(folder, int64(len(rawMsg)), opts)
	if _, err := appendCmd.Write(rawMsg); err != nil {
		_ = appendCmd.Close()
		return err
	}
	if err := appendCmd.Close(); err != nil {
		return err
	}
	_, err = appendCmd.Wait()
	return err
}

// SearchSubject searches a folder for UIDs of messages whose Subject header contains the query string.
func (c *Client) SearchSubject(folder string, query string) (res []uint32, err error) {
	defer c.logCmd("SEARCH", "folder", folder, "query", query)(&err)
	if _, err := c.cli.Select(folder, nil).Wait(); err != nil {
		return nil, err
	}
	searchCmd := c.cli.UIDSearch(&imap.SearchCriteria{
		Header: []imap.SearchCriteriaHeaderField{{Key: "Subject", Value: query}},
	}, nil)
	data, err := searchCmd.Wait()
	if err != nil {
		return nil, err
	}
	uids := data.AllUIDs()
	res = make([]uint32, 0, len(uids))
	for _, u := range uids {
		res = append(res, uint32(u))
	}
	return res, nil
}

// StagingMessage holds basic metadata and raw body for a staging message.
type StagingMessage struct {
	UID          uint32
	InternalDate time.Time
	Body         []byte
}

// FetchStagingMessages fetches raw body and internal date for a set of UIDs.
func (c *Client) FetchStagingMessages(folder string, uids []uint32) (res []StagingMessage, err error) {
	defer c.logCmd("FETCH_STAGING", "folder", folder, "uids_count", len(uids))(&err)
	if _, err := c.cli.Select(folder, nil).Wait(); err != nil {
		return nil, err
	}
	var uidSet imap.UIDSet
	for _, u := range uids {
		uidSet.AddNum(imap.UID(u))
	}

	bodySection := &imap.FetchItemBodySection{Peek: true}
	fetchCmd := c.cli.Fetch(uidSet, &imap.FetchOptions{
		BodySection:  []*imap.FetchItemBodySection{bodySection},
		InternalDate: true,
		UID:          true,
	})
	msgs, err := fetchCmd.Collect()
	if err != nil {
		return nil, err
	}

	for _, msg := range msgs {
		raw := msg.FindBodySection(bodySection)
		res = append(res, StagingMessage{
			UID:          uint32(msg.UID),
			InternalDate: msg.InternalDate,
			Body:         raw,
		})
	}
	return res, nil
}

// MarkDeletedAndExpunge flags specified UIDs as \Deleted and expunges them from the folder.
func (c *Client) MarkDeletedAndExpunge(folder string, uids []uint32) (err error) {
	defer c.logCmd("EXPUNGE_DELETED", "folder", folder, "uids_count", len(uids))(&err)
	if len(uids) == 0 {
		return nil
	}
	if _, err := c.cli.Select(folder, nil).Wait(); err != nil {
		return err
	}
	var uidSet imap.UIDSet
	for _, u := range uids {
		uidSet.AddNum(imap.UID(u))
	}

	storeCmd := c.cli.Store(uidSet, &imap.StoreFlags{
		Op:     imap.StoreFlagsAdd,
		Flags:  []imap.Flag{imap.FlagDeleted},
		Silent: true,
	}, nil)
	if _, err := storeCmd.Collect(); err != nil {
		return err
	}
	_, err = c.cli.Expunge().Collect()
	return err
}

// FetchRawMessageByUID fetches the raw RFC 822 body of a message by UID.
func (c *Client) FetchRawMessageByUID(folder string, uid uint32) (res []byte, err error) {
	defer c.logCmd("FETCH_RAW", "folder", folder, "uid", uid)(&err)
	if _, err := c.cli.Select(folder, nil).Wait(); err != nil {
		return nil, err
	}
	bodySection := &imap.FetchItemBodySection{Peek: true}
	var uidSet imap.UIDSet
	uidSet.AddNum(imap.UID(uid))
	fetchCmd := c.cli.Fetch(uidSet, &imap.FetchOptions{
		BodySection: []*imap.FetchItemBodySection{bodySection},
	})
	msgs, err := fetchCmd.Collect()
	if err != nil || len(msgs) == 0 {
		return nil, fmt.Errorf("message not found")
	}
	raw := msgs[0].FindBodySection(bodySection)
	if len(raw) == 0 {
		return nil, fmt.Errorf("empty body")
	}
	return raw, nil
}

// AppendAndGetUID appends a raw RFC 822 message to a folder and returns the assigned UID.
func (c *Client) AppendAndGetUID(folder string, rawMsg []byte, flags []string, t time.Time) (uid uint32, err error) {
	defer c.logCmd("APPEND_GET_UID", "folder", folder, "size", len(rawMsg))(&err)
	var imapFlags []imap.Flag
	for _, f := range flags {
		imapFlags = append(imapFlags, imap.Flag(f))
	}
	opts := &imap.AppendOptions{
		Flags: imapFlags,
		Time:  t,
	}
	appendCmd := c.cli.Append(folder, int64(len(rawMsg)), opts)
	if _, err := appendCmd.Write(rawMsg); err != nil {
		_ = appendCmd.Close()
		return 0, err
	}
	if err := appendCmd.Close(); err != nil {
		return 0, err
	}
	appendData, err := appendCmd.Wait()
	if err != nil {
		return 0, err
	}
	if appendData != nil && appendData.UID != 0 {
		return uint32(appendData.UID), nil
	}
	statusCmd := c.cli.Status(folder, &imap.StatusOptions{UIDNext: true})
	if status, err := statusCmd.Wait(); err == nil && status.UIDNext > 1 {
		return uint32(status.UIDNext - 1), nil
	}
	return 1, nil
}

// SetFlagsByUID sets the flags for a specific UID in a folder.
func (c *Client) SetFlagsByUID(folder string, uid uint32, flags []string) (err error) {
	defer c.logCmd("STORE_FLAGS_SET", "folder", folder, "uid", uid, "flags", flags)(&err)
	if _, err := c.cli.Select(folder, nil).Wait(); err != nil {
		return err
	}
	var uidSet imap.UIDSet
	uidSet.AddNum(imap.UID(uid))
	var imapFlags []imap.Flag
	for _, f := range flags {
		imapFlags = append(imapFlags, imap.Flag(f))
	}
	storeCmd := c.cli.Store(uidSet, &imap.StoreFlags{
		Op:     imap.StoreFlagsSet,
		Flags:  imapFlags,
		Silent: true,
	}, nil)
	_, err = storeCmd.Collect()
	return err
}

// AddFlagsByUID adds flags to a message by UID.
func (c *Client) AddFlagsByUID(folder string, uid uint32, flags []string) (err error) {
	defer c.logCmd("STORE_FLAGS_ADD", "folder", folder, "uid", uid, "flags", flags)(&err)
	if _, err := c.cli.Select(folder, nil).Wait(); err != nil {
		return err
	}
	var uidSet imap.UIDSet
	uidSet.AddNum(imap.UID(uid))
	var imapFlags []imap.Flag
	for _, f := range flags {
		imapFlags = append(imapFlags, imap.Flag(f))
	}
	storeCmd := c.cli.Store(uidSet, &imap.StoreFlags{
		Op:     imap.StoreFlagsAdd,
		Flags:  imapFlags,
		Silent: true,
	}, nil)
	_, err = storeCmd.Collect()
	return err
}

// RemoveFlagsByUID removes flags from a message by UID.
func (c *Client) RemoveFlagsByUID(folder string, uid uint32, flags []string) (err error) {
	defer c.logCmd("STORE_FLAGS_DEL", "folder", folder, "uid", uid, "flags", flags)(&err)
	if _, err := c.cli.Select(folder, nil).Wait(); err != nil {
		return err
	}
	var uidSet imap.UIDSet
	uidSet.AddNum(imap.UID(uid))
	var imapFlags []imap.Flag
	for _, f := range flags {
		imapFlags = append(imapFlags, imap.Flag(f))
	}
	storeCmd := c.cli.Store(uidSet, &imap.StoreFlags{
		Op:     imap.StoreFlagsDel,
		Flags:  imapFlags,
		Silent: true,
	}, nil)
	_, err = storeCmd.Collect()
	return err
}

// MoveByUID moves a message by UID to a destination folder and returns the new UID if known.
func (c *Client) MoveByUID(folder string, uid uint32, destFolder string) (newUID uint32, err error) {
	defer c.logCmd("MOVE", "folder", folder, "uid", uid, "dest", destFolder)(&err)
	if _, err := c.cli.Select(folder, nil).Wait(); err != nil {
		return 0, err
	}
	var uidSet imap.UIDSet
	uidSet.AddNum(imap.UID(uid))
	moveCmd := c.cli.Move(uidSet, destFolder)
	moveData, err := moveCmd.Wait()
	if err != nil {
		return 0, err
	}
	if moveData != nil && moveData.DestUIDs != nil {
		if destSet, ok := moveData.DestUIDs.(imap.UIDSet); ok {
			if nums, ok := destSet.Nums(); ok && len(nums) > 0 {
				newUID = uint32(nums[0])
			}
		}
	}
	if newUID == 0 {
		if selData, err := c.cli.Select(destFolder, nil).Wait(); err == nil && selData.NumMessages > 0 {
			fetchCmd := c.cli.Fetch(imap.SeqSetNum(selData.NumMessages), &imap.FetchOptions{UID: true})
			if msgs, err := fetchCmd.Collect(); err == nil && len(msgs) > 0 {
				newUID = uint32(msgs[0].UID)
			}
		}
	}
	return newUID, nil
}

// MessageData holds extracted message metadata and raw RFC 822 body.
type MessageData struct {
	UID          uint32
	Flags        []string
	InternalDate time.Time
	Subject      string
	Body         []byte
}

// FetchMessagesByUIDs fetches raw body, flags, internal date, and subject for specified UIDs in a folder.
func (c *Client) FetchMessagesByUIDs(folder string, uids []uint32) (res []MessageData, err error) {
	defer c.logCmd("FETCH_UIDS", "folder", folder, "uids_count", len(uids))(&err)
	if len(uids) == 0 {
		return nil, nil
	}
	if _, err := c.cli.Select(folder, nil).Wait(); err != nil {
		return nil, err
	}
	var uidSet imap.UIDSet
	for _, u := range uids {
		uidSet.AddNum(imap.UID(u))
	}
	bodySection := &imap.FetchItemBodySection{Peek: true}
	fetchCmd := c.cli.Fetch(uidSet, &imap.FetchOptions{
		BodySection:  []*imap.FetchItemBodySection{bodySection},
		Flags:        true,
		InternalDate: true,
		Envelope:     true,
		UID:          true,
	})
	msgs, err := fetchCmd.Collect()
	if err != nil {
		return nil, err
	}
	for _, msg := range msgs {
		raw := msg.FindBodySection(bodySection)
		var flags []string
		for _, f := range msg.Flags {
			flags = append(flags, string(f))
		}
		subj := ""
		if msg.Envelope != nil {
			subj = msg.Envelope.Subject
		}
		res = append(res, MessageData{
			UID:          uint32(msg.UID),
			Flags:        flags,
			InternalDate: msg.InternalDate,
			Subject:      subj,
			Body:         raw,
		})
	}
	return res, nil
}

// FetchAllMessages fetches all messages in a folder.
func (c *Client) FetchAllMessages(folder string) (res []MessageData, err error) {
	defer c.logCmd("FETCH_ALL", "folder", folder)(&err)
	selData, err := c.cli.Select(folder, nil).Wait()
	if err != nil || selData.NumMessages == 0 {
		return nil, err
	}
	var seqSet imap.SeqSet
	seqSet.AddRange(1, selData.NumMessages)
	bodySection := &imap.FetchItemBodySection{Peek: true}
	fetchCmd := c.cli.Fetch(seqSet, &imap.FetchOptions{
		BodySection:  []*imap.FetchItemBodySection{bodySection},
		Flags:        true,
		InternalDate: true,
		Envelope:     true,
		UID:          true,
	})
	msgs, err := fetchCmd.Collect()
	if err != nil {
		return nil, err
	}
	for _, msg := range msgs {
		raw := msg.FindBodySection(bodySection)
		var flags []string
		for _, f := range msg.Flags {
			flags = append(flags, string(f))
		}
		subj := ""
		if msg.Envelope != nil {
			subj = msg.Envelope.Subject
		}
		res = append(res, MessageData{
			UID:          uint32(msg.UID),
			Flags:        flags,
			InternalDate: msg.InternalDate,
			Subject:      subj,
			Body:         raw,
		})
	}
	return res, nil
}
