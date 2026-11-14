package siteingest

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const telegramAPI = "https://api.telegram.org"

// TelegramClient is a minimal Bot API client. The bot token is a server
// secret and is never logged or returned to browsers.
type TelegramClient struct {
	Token   string
	BaseURL string
	Client  *http.Client
}

type telegramResponse struct {
	OK          bool            `json:"ok"`
	Description string          `json:"description"`
	Result      json.RawMessage `json:"result"`
}

// TelegramUpdate is the subset of an update the linker needs.
type TelegramUpdate struct {
	UpdateID int64 `json:"update_id"`
	Message  *struct {
		Text string `json:"text"`
		Chat struct {
			ID        int64  `json:"id"`
			Title     string `json:"title"`
			FirstName string `json:"first_name"`
			Username  string `json:"username"`
		} `json:"chat"`
	} `json:"message"`
}

func (c *TelegramClient) endpoint(method string) string {
	base := c.BaseURL
	if base == "" {
		base = telegramAPI
	}
	return strings.TrimRight(base, "/") + "/bot" + c.Token + "/" + method
}

func (c *TelegramClient) httpClient(timeout time.Duration) *http.Client {
	if c.Client != nil {
		return c.Client
	}
	return &http.Client{Timeout: timeout}
}

func (c *TelegramClient) do(request *http.Request, timeout time.Duration) (json.RawMessage, error) {
	response, err := c.httpClient(timeout).Do(request)
	if err != nil {
		// Never wrap the URL error: it would contain the bot token.
		return nil, errors.New("telegram request failed")
	}
	defer response.Body.Close()
	var decoded telegramResponse
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("telegram response invalid (HTTP %d)", response.StatusCode)
	}
	if !decoded.OK {
		return nil, fmt.Errorf("telegram rejected request: %s", decoded.Description)
	}
	return decoded.Result, nil
}

func (c *TelegramClient) SendMessage(ctx context.Context, chatID int64, text string) error {
	body, err := json.Marshal(map[string]any{"chat_id": chatID, "text": text, "disable_web_page_preview": true})
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint("sendMessage"), bytes.NewReader(body))
	if err != nil {
		return errors.New("build telegram request failed")
	}
	request.Header.Set("Content-Type", "application/json")
	_, err = c.do(request, 15*time.Second)
	return err
}

func (c *TelegramClient) SendPhoto(ctx context.Context, chatID int64, caption string, jpeg []byte) error {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("chat_id", strconv.FormatInt(chatID, 10))
	_ = writer.WriteField("caption", caption)
	part, err := writer.CreateFormFile("photo", "snapshot.jpg")
	if err != nil {
		return err
	}
	if _, err := part.Write(jpeg); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint("sendPhoto"), &body)
	if err != nil {
		return errors.New("build telegram request failed")
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	_, err = c.do(request, 30*time.Second)
	return err
}

func (c *TelegramClient) GetUpdates(ctx context.Context, offset int64, waitSeconds int) ([]TelegramUpdate, error) {
	query := url.Values{}
	query.Set("offset", strconv.FormatInt(offset, 10))
	query.Set("timeout", strconv.Itoa(waitSeconds))
	query.Set("allowed_updates", `["message"]`)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint("getUpdates")+"?"+query.Encode(), nil)
	if err != nil {
		return nil, errors.New("build telegram request failed")
	}
	result, err := c.do(request, time.Duration(waitSeconds+15)*time.Second)
	if err != nil {
		return nil, err
	}
	var updates []TelegramUpdate
	if err := json.Unmarshal(result, &updates); err != nil {
		return nil, errors.New("telegram updates invalid")
	}
	return updates, nil
}

// TelegramTarget is one chat that receives a site's alerts.
type TelegramTarget struct {
	ID          string    `json:"id"`
	TenantID    string    `json:"tenant_id"`
	SiteID      string    `json:"site_id"`
	ChatID      int64     `json:"chat_id"`
	ChatTitle   string    `json:"chat_title"`
	MinSeverity string    `json:"min_severity"`
	Muted       bool      `json:"muted"`
	CreatedAt   time.Time `json:"created_at"`
}

type TargetStore interface {
	Targets(ctx context.Context, tenantID, siteID string) ([]TelegramTarget, error)
	CreateLinkCode(ctx context.Context, tenantID, siteID, actorID string) (string, time.Time, error)
	Redeem(ctx context.Context, code string, chatID int64, title string) (tenantID, siteID string, ok bool, err error)
	Delete(ctx context.Context, tenantID, targetID string) error
}

// LinkCodeTTL is how long a dashboard-issued Telegram link code stays valid.
const LinkCodeTTL = 15 * time.Minute

const linkAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// NewLinkCode returns an 8-character code that is easy to read and type.
func NewLinkCode() (string, error) {
	buffer := make([]byte, 8)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	for index, value := range buffer {
		buffer[index] = linkAlphabet[int(value)%len(linkAlphabet)]
	}
	return string(buffer), nil
}

func normalizeLinkCode(code string) string {
	return strings.ToUpper(strings.TrimSpace(code))
}

type PostgresTargetStore struct{ pool transactionPool }

func NewPostgresTargetStore(pool transactionPool) *PostgresTargetStore {
	return &PostgresTargetStore{pool: pool}
}

func (s *PostgresTargetStore) Targets(ctx context.Context, tenantID, siteID string) ([]TelegramTarget, error) {
	tx, err := beginTenantTx(ctx, s.pool, tenantID, pgx.ReadOnly)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `
		SELECT id::text, tenant_id::text, site_id::text, chat_id, chat_title, min_severity, muted, created_at
		FROM notify.telegram_targets
		WHERE ($1::uuid IS NULL OR site_id = $1::uuid)
		ORDER BY created_at
		LIMIT 50`, nullableUUID(siteID))
	if err != nil {
		return nil, fmt.Errorf("list telegram targets: %w", err)
	}
	defer rows.Close()
	targets := make([]TelegramTarget, 0)
	for rows.Next() {
		var target TelegramTarget
		if err := rows.Scan(&target.ID, &target.TenantID, &target.SiteID, &target.ChatID, &target.ChatTitle, &target.MinSeverity, &target.Muted, &target.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan telegram target: %w", err)
		}
		targets = append(targets, target)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate telegram targets: %w", err)
	}
	return targets, tx.Commit(ctx)
}

func (s *PostgresTargetStore) CreateLinkCode(ctx context.Context, tenantID, siteID, actorID string) (string, time.Time, error) {
	code, err := NewLinkCode()
	if err != nil {
		return "", time.Time{}, err
	}
	expiresAt := time.Now().UTC().Add(LinkCodeTTL)
	tx, err := beginTenantTx(ctx, s.pool, tenantID, pgx.ReadWrite)
	if err != nil {
		return "", time.Time{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM config.sites WHERE tenant_id = $1::uuid AND id = $2::uuid AND status <> 'retired')`, tenantID, siteID).Scan(&exists); err != nil {
		return "", time.Time{}, fmt.Errorf("validate link site: %w", err)
	}
	if !exists {
		return "", time.Time{}, ErrSiteNotFound
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO notify.telegram_link_codes (code_hash, tenant_id, site_id, created_by, expires_at)
		VALUES ($1, $2::uuid, $3::uuid, $4, $5)`, HashKey(code), tenantID, siteID, actorID, expiresAt); err != nil {
		return "", time.Time{}, fmt.Errorf("store link code: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return "", time.Time{}, fmt.Errorf("commit link code: %w", err)
	}
	return code, expiresAt, nil
}

func (s *PostgresTargetStore) Redeem(ctx context.Context, code string, chatID int64, title string) (string, string, bool, error) {
	code = normalizeLinkCode(code)
	if len(code) != 8 {
		return "", "", false, nil
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadWrite})
	if err != nil {
		return "", "", false, fmt.Errorf("begin link redemption: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var tenantID, siteID string
	err = tx.QueryRow(ctx, `SELECT tenant_id::text, site_id::text FROM notify.redeem_telegram_link($1, $2, $3)`, HashKey(code), chatID, title).Scan(&tenantID, &siteID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, fmt.Errorf("redeem link code: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return "", "", false, fmt.Errorf("commit link redemption: %w", err)
	}
	return tenantID, siteID, true, nil
}

func (s *PostgresTargetStore) Delete(ctx context.Context, tenantID, targetID string) error {
	tx, err := beginTenantTx(ctx, s.pool, tenantID, pgx.ReadWrite)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `DELETE FROM notify.telegram_targets WHERE id = $1::uuid`, targetID)
	if err != nil {
		return fmt.Errorf("delete telegram target: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return ErrKeyNotFound
	}
	return tx.Commit(ctx)
}

// RunTelegramLinker long-polls the bot for "/start CODE" messages and links
// the sending chat to the site that issued the code. It returns when ctx ends.
func RunTelegramLinker(ctx context.Context, client *TelegramClient, store TargetStore, sites func(context.Context, string, string) string, logf func(string, ...any)) {
	var offset int64
	for ctx.Err() == nil {
		updates, err := client.GetUpdates(ctx, offset, 25)
		if err != nil {
			if ctx.Err() == nil {
				logf("telegram linker poll failed: %v", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(10 * time.Second):
			}
			continue
		}
		for _, update := range updates {
			if update.UpdateID >= offset {
				offset = update.UpdateID + 1
			}
			handleTelegramUpdate(ctx, client, store, sites, update, logf)
		}
	}
}

func handleTelegramUpdate(ctx context.Context, client *TelegramClient, store TargetStore, sites func(context.Context, string, string) string, update TelegramUpdate, logf func(string, ...any)) {
	if update.Message == nil {
		return
	}
	fields := strings.Fields(update.Message.Text)
	if len(fields) == 0 || !strings.HasPrefix(fields[0], "/start") {
		return
	}
	chat := update.Message.Chat
	if len(fields) < 2 {
		_ = client.SendMessage(ctx, chat.ID, "Welcome to SyncCam AI alerts. Open Configuration â†’ Telegram alerts in your SyncCam dashboard, press \"Link Telegram\", and send the code shown there as: /start CODE")
		return
	}
	title := chat.Title
	if title == "" {
		title = strings.TrimSpace(chat.FirstName + " " + chat.Username)
	}
	tenantID, siteID, ok, err := store.Redeem(ctx, fields[1], chat.ID, title)
	if err != nil {
		logf("telegram link redemption failed: %v", err)
		_ = client.SendMessage(ctx, chat.ID, "Linking failed because of a temporary problem. Please try again in a minute.")
		return
	}
	if !ok {
		_ = client.SendMessage(ctx, chat.ID, "That code is invalid or expired. Create a new code in the SyncCam dashboard.")
		return
	}
	name := "your site"
	if sites != nil {
		if label := sites(ctx, tenantID, siteID); label != "" {
			name = label
		}
	}
	_ = client.SendMessage(ctx, chat.ID, "âœ… Linked. This chat will now receive SyncCam AI alerts with photos for "+name+".")
}
