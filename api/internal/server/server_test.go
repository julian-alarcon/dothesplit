package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/julian-alarcon/dothesplit/api/internal/config"
	"github.com/julian-alarcon/dothesplit/api/internal/crypto"
	"github.com/julian-alarcon/dothesplit/api/internal/handlers"
	"github.com/julian-alarcon/dothesplit/api/internal/repo"
	"github.com/julian-alarcon/dothesplit/api/internal/server"
	"github.com/julian-alarcon/dothesplit/api/internal/service"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpg "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// readMigrations concatenates every *.up.sql file in migrations/ in filename order.
func readMigrations(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	dir := filepath.Join(filepath.Dir(file), "..", "..", "migrations")
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	var out bytes.Buffer
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".up.sql") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		require.NoError(t, err)
		out.Write(b)
		out.WriteString("\n")
	}
	return out.String()
}

type testStack struct {
	srv  *httptest.Server
	pool *pgxpool.Pool
	ctr  testcontainers.Container
}

func setup(t *testing.T) *testStack {
	t.Helper()
	ctx := context.Background()

	pgc, err := tcpg.Run(ctx,
		"postgres:16-alpine",
		tcpg.WithDatabase("dts"),
		tcpg.WithUsername("dts"),
		tcpg.WithPassword("dts"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(60*time.Second),
		),
	)
	require.NoError(t, err)

	dsn, err := pgc.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, readMigrations(t))
	require.NoError(t, err)

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	cfg := &config.Config{
		DatabaseURL:    dsn,
		SessionTTLDay:  30,
		EmailEncKey:    key,
		EmailHMACKey:   key,
		PasswordPepper: key,
	}
	email, err := crypto.NewEmailCipher(cfg.EmailEncKey, cfg.EmailHMACKey)
	require.NoError(t, err)

	users := repo.NewUserRepo(pool)
	groups := repo.NewGroupRepo(pool)
	expenses := repo.NewExpenseRepo(pool)
	settlements := repo.NewSettlementRepo(pool)
	balances := repo.NewBalanceRepo(pool)
	recurring := repo.NewRecurringRepo(pool)
	categories := repo.NewCategoryRepo(pool)
	categorySvc := service.NewCategoryService(categories)
	activityRepo := repo.NewActivityRepo(pool)

	sessionRepo := repo.NewSessionRepo(pool)
	ttl := time.Duration(cfg.SessionTTLDay) * 24 * time.Hour
	groupSvc := service.NewGroupService(groups, users, balances, email)
	h := server.New(&handlers.Server{
		Cfg:         cfg,
		Pool:        pool,
		Auth:        service.NewAuthService(users, sessionRepo, email, cfg.PasswordPepper, ttl),
		MeSvc:       service.NewMeService(users, sessionRepo, email, cfg.PasswordPepper),
		Groups:      groupSvc,
		Categories:  categorySvc,
		Expenses:    service.NewExpenseService(expenses, groups, categorySvc),
		Balances:    service.NewBalanceService(balances, groups),
		Settlements: service.NewSettlementService(settlements, groups),
		Recurring:   service.NewRecurringService(recurring, expenses, groups, categorySvc),
		Activity:    service.NewActivityService(groupSvc, activityRepo, expenses, settlements, recurring),
	})
	srv := httptest.NewServer(h)

	ts := &testStack{srv: srv, pool: pool, ctr: pgc}
	t.Cleanup(func() {
		srv.Close()
		pool.Close()
		_ = pgc.Terminate(context.Background())
	})
	return ts
}

// request is a tiny helper used throughout the test.
func request(t *testing.T, method, url string, body any, cookie *http.Cookie) (*http.Response, map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		require.NoError(t, json.NewEncoder(&buf).Encode(body))
	}
	req, err := http.NewRequest(method, url, &buf)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	var out map[string]any
	if resp.Body != nil {
		defer resp.Body.Close()
		_ = json.NewDecoder(resp.Body).Decode(&out)
	}
	return resp, out
}

// rawRequest issues the same kind of authenticated call as request(), but
// returns the live response so callers can read headers / non-JSON bodies.
// Useful for the avatar download (image/png) and Set-Cookie inspection.
func rawRequest(t *testing.T, method, url string, body any, cookie *http.Cookie) (*http.Response, []byte) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		require.NoError(t, json.NewEncoder(&buf).Encode(body))
	}
	req, err := http.NewRequest(method, url, &buf)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	var data []byte
	if resp.Body != nil {
		defer resp.Body.Close()
		data, _ = io.ReadAll(resp.Body)
	}
	return resp, data
}

// requestList is like request but decodes the body as a JSON array.
func requestList(t *testing.T, method, url string, body any, cookie *http.Cookie) (*http.Response, []map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		require.NoError(t, json.NewEncoder(&buf).Encode(body))
	}
	req, err := http.NewRequest(method, url, &buf)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	var out []map[string]any
	if resp.Body != nil {
		defer resp.Body.Close()
		_ = json.NewDecoder(resp.Body).Decode(&out)
	}
	return resp, out
}

func sessionCookie(resp *http.Response) *http.Cookie {
	for _, c := range resp.Cookies() {
		if (c.Name == "__Host-dts_session" || c.Name == "dts_session") && c.Value != "" {
			return c
		}
	}
	return nil
}

func registerUser(t *testing.T, base, email, pw, name string) (map[string]any, *http.Cookie) {
	t.Helper()
	resp, body := request(t, "POST", base+"/v1/auth/register", map[string]any{
		"email":        email,
		"password":     pw,
		"display_name": name,
	}, nil)
	require.Equal(t, http.StatusCreated, resp.StatusCode, body)
	c := sessionCookie(resp)
	require.NotNil(t, c)
	return body, c
}

// TestGoldenPath exercises the full MVP flow end-to-end against a real Postgres:
// auth (register/login/logout), groups + membership, expenses with three split
// modes, balance computation, simplified debts, settlements, and authz negatives.
func TestGoldenPath(t *testing.T) {
	ts := setup(t)
	base := ts.srv.URL

	// --- Auth ---
	userA, cookieA := registerUser(t, base, "a@test.dev", "passwordpassword", "Alice")
	userB, cookieB := registerUser(t, base, "b@test.dev", "passwordpassword", "Bob")

	// /me works with session, fails without
	resp, me := request(t, "GET", base+"/v1/me", nil, cookieA)
	require.Equal(t, http.StatusOK, resp.StatusCode, me)
	require.Equal(t, "Alice", me["display_name"])
	// Defaults: week_start = 1 (Monday).
	require.EqualValues(t, 1, me["week_start"])

	// PATCH /v1/me - flip week_start to Sunday.
	resp, updMe := request(t, "PATCH", base+"/v1/me", map[string]any{"week_start": 0}, cookieA)
	require.Equal(t, http.StatusOK, resp.StatusCode, updMe)
	require.EqualValues(t, 0, updMe["week_start"])
	require.Equal(t, "Alice", updMe["display_name"]) // unchanged

	// Invalid week_start (outside enum) → 400.
	resp, _ = request(t, "PATCH", base+"/v1/me", map[string]any{"week_start": 2}, cookieA)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)

	// Empty PATCH → 400.
	resp, _ = request(t, "PATCH", base+"/v1/me", map[string]any{}, cookieA)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)

	// Restore for downstream tests.
	resp, _ = request(t, "PATCH", base+"/v1/me", map[string]any{"week_start": 1}, cookieA)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	resp, _ = request(t, "GET", base+"/v1/me", nil, nil)
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)

	// Duplicate register → 409
	resp, _ = request(t, "POST", base+"/v1/auth/register", map[string]any{
		"email": "a@test.dev", "password": "passwordpassword", "display_name": "Alice2",
	}, nil)
	require.Equal(t, http.StatusConflict, resp.StatusCode)

	// Bad password → 401
	resp, _ = request(t, "POST", base+"/v1/auth/login", map[string]any{
		"email": "a@test.dev", "password": "wrongwrongwrong",
	}, nil)
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)

	// --- Groups ---
	resp, groupBody := request(t, "POST", base+"/v1/groups",
		map[string]any{"name": "Trip"}, cookieA)
	require.Equal(t, http.StatusCreated, resp.StatusCode, groupBody)
	groupID := groupBody["id"].(string)

	// A adds B
	resp, _ = request(t, "POST", base+"/v1/groups/"+groupID+"/members",
		map[string]any{"email": "b@test.dev"}, cookieA)
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	// A tries to invite unregistered → 404
	resp, _ = request(t, "POST", base+"/v1/groups/"+groupID+"/members",
		map[string]any{"email": "ghost@test.dev"}, cookieA)
	require.Equal(t, http.StatusNotFound, resp.StatusCode)

	// --- Default split (2-member group) ---
	// Pin a 60/40 default; should round-trip on the GET.
	resp, _ = request(t, "PATCH", base+"/v1/groups/"+groupID, map[string]any{
		"default_split": []map[string]any{
			{"user_id": userA["id"], "basis_points": 6000},
			{"user_id": userB["id"], "basis_points": 4000},
		},
	}, cookieA)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	resp, listed := requestList(t, "GET", base+"/v1/groups", nil, cookieA)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var thisGroup map[string]any
	for _, g := range listed {
		if g["id"] == groupID {
			thisGroup = g
		}
	}
	require.NotNil(t, thisGroup)
	ds := thisGroup["default_split"].([]any)
	require.Len(t, ds, 2)

	// Sum != 10000 → 400.
	resp, _ = request(t, "PATCH", base+"/v1/groups/"+groupID, map[string]any{
		"default_split": []map[string]any{
			{"user_id": userA["id"], "basis_points": 5000},
			{"user_id": userB["id"], "basis_points": 4000},
		},
	}, cookieA)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)

	// Adding a 3rd member auto-clears the default.
	_, cookieD := registerUser(t, base, "d@test.dev", "passwordpassword", "Dora")
	_ = cookieD
	resp, _ = request(t, "POST", base+"/v1/groups/"+groupID+"/members",
		map[string]any{"email": "d@test.dev"}, cookieA)
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	resp, listed = requestList(t, "GET", base+"/v1/groups", nil, cookieA)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	for _, g := range listed {
		if g["id"] == groupID {
			require.Nil(t, g["default_split"], "default_split should auto-clear when group grows past 2")
		}
	}

	// Pinning a default on a 3-member group → 400.
	resp, _ = request(t, "PATCH", base+"/v1/groups/"+groupID, map[string]any{
		"default_split": []map[string]any{
			{"user_id": userA["id"], "basis_points": 6000},
			{"user_id": userB["id"], "basis_points": 4000},
		},
	}, cookieA)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)

	// --- Expenses: equal split ---
	resp, e1 := request(t, "POST", base+"/v1/groups/"+groupID+"/expenses", map[string]any{
		"description":  "Hotel",
		"amount_cents": 20000,
		"payer_id":     userA["id"],
		"mode":         "equal",
		"splits": []map[string]any{
			{"user_id": userA["id"]}, {"user_id": userB["id"]},
		},
	}, cookieA)
	require.Equal(t, http.StatusCreated, resp.StatusCode, e1)
	hotelID := e1["id"].(string)

	// Balances: A +100, B -100
	resp, bal := request(t, "GET", base+"/v1/groups/"+groupID+"/balances", nil, cookieA)
	require.Equal(t, http.StatusOK, resp.StatusCode, bal)
	nets := netMap(bal)
	require.EqualValues(t, 10000, nets[userA["id"].(string)])
	require.EqualValues(t, -10000, nets[userB["id"].(string)])

	simp := bal["simplified"].([]any)
	require.Len(t, simp, 1)
	d := simp[0].(map[string]any)
	require.Equal(t, userB["id"], d["from_user_id"])
	require.Equal(t, userA["id"], d["to_user_id"])
	require.EqualValues(t, 10000, d["amount_cents"])

	// --- Expenses: exact split ---
	resp, _ = request(t, "POST", base+"/v1/groups/"+groupID+"/expenses", map[string]any{
		"description":  "Food",
		"amount_cents": 4000,
		"payer_id":     userB["id"],
		"mode":         "exact",
		"splits": []map[string]any{
			{"user_id": userA["id"], "value": 1000},
			{"user_id": userB["id"], "value": 3000},
		},
	}, cookieB)
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	// A paid 200, owes 100+10 = 110, net = +90
	_, bal = request(t, "GET", base+"/v1/groups/"+groupID+"/balances", nil, cookieA)
	nets = netMap(bal)
	require.EqualValues(t, 9000, nets[userA["id"].(string)])
	require.EqualValues(t, -9000, nets[userB["id"].(string)])

	// Exact split that doesn't sum → 400
	resp, _ = request(t, "POST", base+"/v1/groups/"+groupID+"/expenses", map[string]any{
		"description":  "Bad",
		"amount_cents": 1000,
		"payer_id":     userA["id"],
		"mode":         "exact",
		"splits": []map[string]any{
			{"user_id": userA["id"], "value": 500},
			{"user_id": userB["id"], "value": 400},
		},
	}, cookieA)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)

	// --- Settlement: B pays A $50 ---
	resp, settlementBody := request(t, "POST", base+"/v1/groups/"+groupID+"/settlements", map[string]any{
		"to_user_id":   userA["id"],
		"amount_cents": 5000,
	}, cookieB)
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	settlementID := settlementBody["id"].(string)

	_, bal = request(t, "GET", base+"/v1/groups/"+groupID+"/balances", nil, cookieA)
	nets = netMap(bal)
	require.EqualValues(t, 4000, nets[userA["id"].(string)])
	require.EqualValues(t, -4000, nets[userB["id"].(string)])

	// --- Settlement delete authz: any group member can delete; non-member cannot ---
	_, cookieStranger := registerUser(t, base, "stranger-settle@test.dev", "passwordpassword", "StrangerSettle")
	resp, _ = request(t, "DELETE", base+"/v1/settlements/"+settlementID, nil, cookieStranger)
	require.Equal(t, http.StatusForbidden, resp.StatusCode)
	// A (the recipient, a regular member) deletes B's settlement; balance reverts.
	resp, _ = request(t, "DELETE", base+"/v1/settlements/"+settlementID, nil, cookieA)
	require.Equal(t, http.StatusNoContent, resp.StatusCode)
	resp, _ = request(t, "DELETE", base+"/v1/settlements/"+settlementID, nil, cookieA)
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
	_, bal = request(t, "GET", base+"/v1/groups/"+groupID+"/balances", nil, cookieA)
	nets = netMap(bal)
	require.EqualValues(t, 9000, nets[userA["id"].(string)])
	require.EqualValues(t, -9000, nets[userB["id"].(string)])
	// Re-create the settlement so subsequent assertions in this test still hold.
	resp, settlementBody = request(t, "POST", base+"/v1/groups/"+groupID+"/settlements", map[string]any{
		"to_user_id":   userA["id"],
		"amount_cents": 5000,
	}, cookieB)
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	_ = settlementBody

	// --- Categories + expense edits ---
	resp, catsList := requestList(t, "GET", base+"/v1/categories", nil, cookieA)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.NotEmpty(t, catsList)
	var groceriesID, trainID string
	for _, c := range catsList {
		switch c["slug"] {
		case "groceries":
			groceriesID = c["id"].(string)
		case "train":
			trainID = c["id"].(string)
		}
	}
	require.NotEmpty(t, groceriesID)
	require.NotEmpty(t, trainID)

	// Expense created without a category → defaults to "other".
	resp, exp := request(t, "POST", base+"/v1/groups/"+groupID+"/expenses", map[string]any{
		"description":  "Bus",
		"amount_cents": 1000,
		"payer_id":     userA["id"],
		"mode":         "equal",
		"splits":       []map[string]any{{"user_id": userA["id"]}, {"user_id": userB["id"]}},
	}, cookieA)
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	busID := exp["id"].(string)
	require.NotEmpty(t, exp["category_id"])

	// PATCH: rename + change category + change amount. 1000→2000 doubles each share.
	resp, upd := request(t, "PATCH", base+"/v1/expenses/"+busID, map[string]any{
		"description":  "Train",
		"amount_cents": 2000,
		"category_id":  trainID,
	}, cookieA)
	require.Equal(t, http.StatusOK, resp.StatusCode, upd)
	require.Equal(t, "Train", upd["description"])
	require.EqualValues(t, 2000, upd["amount_cents"])
	require.Equal(t, trainID, upd["category_id"])
	for _, s := range upd["splits"].([]any) {
		require.EqualValues(t, 1000, s.(map[string]any)["share_cents"])
	}

	// Non-payer group member CAN now edit (authz loosened; history records editor).
	resp, _ = request(t, "PATCH", base+"/v1/expenses/"+busID, map[string]any{
		"description": "Train (edited by B)",
	}, cookieB)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// Non-member cannot edit.
	_, cookieX := registerUser(t, base, "x@test.dev", "passwordpassword", "Mallory")
	resp, _ = request(t, "PATCH", base+"/v1/expenses/"+busID, map[string]any{
		"description": "Hacked",
	}, cookieX)
	require.Equal(t, http.StatusForbidden, resp.StatusCode)

	// Revision log: description (A), amount_cents (A), category_id (A), description (B).
	resp, revs := requestList(t, "GET", base+"/v1/expenses/"+busID+"/revisions", nil, cookieA)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Len(t, revs, 4)

	// Change the payer to B. Actor A is still the original payer, so allowed.
	resp, updPayer := request(t, "PATCH", base+"/v1/expenses/"+busID, map[string]any{
		"payer_id": userB["id"],
	}, cookieA)
	require.Equal(t, http.StatusOK, resp.StatusCode, updPayer)
	require.Equal(t, userB["id"], updPayer["payer_id"])

	// After reassigning the payer to B, original payer A loses edit permission
	// unless A is the group creator - A *is* the creator here, so A can still edit.
	// Non-member payer change → 400.
	resp, _ = request(t, "PATCH", base+"/v1/expenses/"+busID, map[string]any{
		"payer_id": "00000000-0000-0000-0000-000000000000",
	}, cookieA)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)

	// Revision log should now have one more row (payer_id) - 5 total.
	resp, revs = requestList(t, "GET", base+"/v1/expenses/"+busID+"/revisions", nil, cookieA)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Len(t, revs, 5)
	last := revs[len(revs)-1]
	require.Equal(t, "payer_id", last["field"])

	// --- incurred_at on update ---
	// Set the bus expense to a specific historical date and verify the
	// revision row captures it. created_at must NOT change.
	preCreatedAt := upd["created_at"].(string)
	newDate := "2025-01-15T12:00:00Z"
	resp, withDate := request(t, "PATCH", base+"/v1/expenses/"+busID, map[string]any{
		"incurred_at": newDate,
	}, cookieA)
	require.Equal(t, http.StatusOK, resp.StatusCode, withDate)
	require.Equal(t, newDate, withDate["incurred_at"])
	require.Equal(t, preCreatedAt, withDate["created_at"], "created_at must remain immutable")

	// Revision log gets a new 'incurred_at' row.
	resp, revs = requestList(t, "GET", base+"/v1/expenses/"+busID+"/revisions", nil, cookieA)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	foundDate := false
	for _, rv := range revs {
		if rv["field"] == "incurred_at" {
			foundDate = true
			require.Contains(t, rv["new_value"], "2025-01-15")
		}
	}
	require.True(t, foundDate, "expected an 'incurred_at' revision row")

	// Re-PATCH with the same value → no new revision row.
	preLen := len(revs)
	resp, _ = request(t, "PATCH", base+"/v1/expenses/"+busID, map[string]any{
		"incurred_at": newDate,
	}, cookieA)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	resp, revs = requestList(t, "GET", base+"/v1/expenses/"+busID+"/revisions", nil, cookieA)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, preLen, len(revs), "no-op date PATCH should not append a revision")

	// --- Non-equal splits on update ---
	// Switch to explicit percent mode: 70/30 on a 2000-cent expense.
	resp, updSplits := request(t, "PATCH", base+"/v1/expenses/"+busID, map[string]any{
		"mode": "percent",
		"splits": []map[string]any{
			{"user_id": userA["id"], "value": 7000},
			{"user_id": userB["id"], "value": 3000},
		},
	}, cookieA)
	require.Equal(t, http.StatusOK, resp.StatusCode, updSplits)
	shareByUser := map[string]int64{}
	for _, s := range updSplits["splits"].([]any) {
		m := s.(map[string]any)
		shareByUser[m["user_id"].(string)] = int64(m["share_cents"].(float64))
	}
	require.EqualValues(t, 1400, shareByUser[userA["id"].(string)])
	require.EqualValues(t, 600, shareByUser[userB["id"].(string)])

	// Revision log now has a 'splits' row.
	resp, revs = requestList(t, "GET", base+"/v1/expenses/"+busID+"/revisions", nil, cookieA)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	foundSplits := false
	for _, rv := range revs {
		if rv["field"] == "splits" {
			foundSplits = true
			var parsed []map[string]any
			require.NoError(t, json.Unmarshal([]byte(rv["new_value"].(string)), &parsed))
			require.Len(t, parsed, 2)
		}
	}
	require.True(t, foundSplits, "expected a 'splits' revision row")

	// Percent that doesn't sum to 100 → 400.
	resp, _ = request(t, "PATCH", base+"/v1/expenses/"+busID, map[string]any{
		"mode": "percent",
		"splits": []map[string]any{
			{"user_id": userA["id"], "value": 5000},
			{"user_id": userB["id"], "value": 4000},
		},
	}, cookieA)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)

	// Mode without splits → 400.
	resp, _ = request(t, "PATCH", base+"/v1/expenses/"+busID, map[string]any{
		"mode": "exact",
	}, cookieA)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)

	// Unknown category → 400.
	resp, _ = request(t, "PATCH", base+"/v1/expenses/"+busID, map[string]any{
		"category_id": "00000000-0000-0000-0000-000000000000",
	}, cookieA)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)

	// Create expense with an explicit category_id.
	resp, _ = request(t, "POST", base+"/v1/groups/"+groupID+"/expenses", map[string]any{
		"description":  "Market",
		"amount_cents": 500,
		"payer_id":     userA["id"],
		"category_id":  groceriesID,
		"mode":         "equal",
		"splits":       []map[string]any{{"user_id": userA["id"]}},
	}, cookieA)
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	// --- Authz on delete ---
	_, cookieC := registerUser(t, base, "c@test.dev", "passwordpassword", "Carol")
	// Non-member authenticated user cannot delete
	resp, _ = request(t, "DELETE", base+"/v1/expenses/"+hotelID, nil, cookieC)
	require.Equal(t, http.StatusForbidden, resp.StatusCode)
	// Any group member (here B — neither creator nor payer of this expense) can delete.
	resp, snack := request(t, "POST", base+"/v1/groups/"+groupID+"/expenses", map[string]any{
		"description":  "Snacks",
		"amount_cents": 200,
		"payer_id":     userA["id"],
		"mode":         "equal",
		"splits":       []map[string]any{{"user_id": userA["id"]}, {"user_id": userB["id"]}},
	}, cookieA)
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	snackID := snack["id"].(string)
	resp, _ = request(t, "DELETE", base+"/v1/expenses/"+snackID, nil, cookieB)
	require.Equal(t, http.StatusNoContent, resp.StatusCode)
	// Payer can delete; also soft-delete reflected in balances
	resp, _ = request(t, "DELETE", base+"/v1/expenses/"+hotelID, nil, cookieA)
	require.Equal(t, http.StatusNoContent, resp.StatusCode)
	// Second delete → 404
	resp, _ = request(t, "DELETE", base+"/v1/expenses/"+hotelID, nil, cookieA)
	require.Equal(t, http.StatusNotFound, resp.StatusCode)

	// --- Transfer ownership ---
	// Fresh group: A is creator, B is a member.
	resp, transferGroupBody := request(t, "POST", base+"/v1/groups",
		map[string]any{"name": "TransferTest"}, cookieA)
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	tgID := transferGroupBody["id"].(string)
	resp, _ = request(t, "POST", base+"/v1/groups/"+tgID+"/members",
		map[string]any{"email": "b@test.dev"}, cookieA)
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	// Non-creator cannot transfer ownership.
	resp, _ = request(t, "PATCH", base+"/v1/groups/"+tgID, map[string]any{
		"created_by": userB["id"],
	}, cookieB)
	require.Equal(t, http.StatusForbidden, resp.StatusCode)

	// Creator cannot hand the group to a non-member.
	stranger, _ := registerUser(t, base, "stranger@test.dev", "passwordpassword", "Stranger")
	resp, _ = request(t, "PATCH", base+"/v1/groups/"+tgID, map[string]any{
		"created_by": stranger["id"],
	}, cookieA)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)

	// Happy path: A transfers to B.
	resp, transferred := request(t, "PATCH", base+"/v1/groups/"+tgID, map[string]any{
		"created_by": userB["id"],
	}, cookieA)
	require.Equal(t, http.StatusOK, resp.StatusCode, transferred)
	require.Equal(t, userB["id"], transferred["created_by"])

	// Now A (former creator) is just a regular member; can no longer transfer.
	resp, _ = request(t, "PATCH", base+"/v1/groups/"+tgID, map[string]any{
		"created_by": userA["id"],
	}, cookieA)
	require.Equal(t, http.StatusForbidden, resp.StatusCode)

	// B (the new creator) can transfer back to A.
	resp, _ = request(t, "PATCH", base+"/v1/groups/"+tgID, map[string]any{
		"created_by": userA["id"],
	}, cookieB)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// --- Remove members / leave group ---
	// Fresh group: A is creator, B and Carol are members.
	resp, removeGroupBody := request(t, "POST", base+"/v1/groups",
		map[string]any{"name": "RemoveTest"}, cookieA)
	require.Equal(t, http.StatusCreated, resp.StatusCode, removeGroupBody)
	rgID := removeGroupBody["id"].(string)
	resp, _ = request(t, "POST", base+"/v1/groups/"+rgID+"/members",
		map[string]any{"email": "b@test.dev"}, cookieA)
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	resp, _ = request(t, "POST", base+"/v1/groups/"+rgID+"/members",
		map[string]any{"email": "c@test.dev"}, cookieA)
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	// Creator cannot leave or be removed.
	resp, _ = request(t, "DELETE", base+"/v1/groups/"+rgID+"/members/"+userA["id"].(string), nil, cookieA)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)

	// Non-creator cannot remove someone else.
	resp, _ = request(t, "DELETE", base+"/v1/groups/"+rgID+"/members/"+userB["id"].(string), nil, cookieC)
	require.Equal(t, http.StatusForbidden, resp.StatusCode)

	// Carol leaves herself (zero balance) → 204; group drops to 2 members.
	resp, carolMe := request(t, "GET", base+"/v1/me", nil, cookieC)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	carolID := carolMe["id"].(string)
	resp, _ = request(t, "DELETE", base+"/v1/groups/"+rgID+"/members/"+carolID, nil, cookieC)
	require.Equal(t, http.StatusNoContent, resp.StatusCode)

	// Carol can no longer access the group.
	resp, _ = request(t, "GET", base+"/v1/groups/"+rgID+"/balances", nil, cookieC)
	require.Equal(t, http.StatusForbidden, resp.StatusCode)

	// Add an expense so B has a non-zero balance.
	resp, _ = request(t, "POST", base+"/v1/groups/"+rgID+"/expenses", map[string]any{
		"description":  "Snack",
		"amount_cents": 1000,
		"payer_id":     userA["id"],
		"mode":         "equal",
		"splits": []map[string]any{
			{"user_id": userA["id"]}, {"user_id": userB["id"]},
		},
	}, cookieA)
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	// Creator cannot remove B while B has a non-zero balance - silently
	// writing off another member's debt is too high a blast radius.
	resp, _ = request(t, "DELETE", base+"/v1/groups/"+rgID+"/members/"+userB["id"].(string), nil, cookieA)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)

	// But B *can* leave themselves even with a non-zero balance - UI surfaces
	// a warning, the user owns the consequence.
	resp, _ = request(t, "DELETE", base+"/v1/groups/"+rgID+"/members/"+userB["id"].(string), nil, cookieB)
	require.Equal(t, http.StatusNoContent, resp.StatusCode)

	// Re-add B and settle up so the rest of the lifecycle still has a target.
	resp, _ = request(t, "POST", base+"/v1/groups/"+rgID+"/members",
		map[string]any{"email": "b@test.dev"}, cookieA)
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	// B settles their share → balance back to zero.
	resp, _ = request(t, "POST", base+"/v1/groups/"+rgID+"/settlements", map[string]any{
		"to_user_id":   userA["id"],
		"amount_cents": 500,
	}, cookieB)
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	// Now creator can remove B.
	resp, _ = request(t, "DELETE", base+"/v1/groups/"+rgID+"/members/"+userB["id"].(string), nil, cookieA)
	require.Equal(t, http.StatusNoContent, resp.StatusCode)

	// Removing a non-member → 404.
	resp, _ = request(t, "DELETE", base+"/v1/groups/"+rgID+"/members/"+userB["id"].(string), nil, cookieA)
	require.Equal(t, http.StatusNotFound, resp.StatusCode)

	// Logout, then cookie is unauthenticated
	resp, _ = request(t, "POST", base+"/v1/auth/logout", nil, cookieA)
	require.Equal(t, http.StatusNoContent, resp.StatusCode)
	resp, _ = request(t, "GET", base+"/v1/me", nil, cookieA)
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)

	// /healthz always open
	resp, _ = request(t, "GET", base+"/healthz", nil, nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
}

// TestActivityFeed exercises the merged /v1/groups/{id}/activity endpoint:
// page size, ordering invariant (newest first), cursor continuation across
// multiple pages, no-overlap guarantee, and 403 for non-members.
func TestActivityFeed(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: needs Docker/testcontainers")
	}
	ts := setup(t)
	base := ts.srv.URL

	userA, cookieA := registerUser(t, base, "feed-a@test.dev", "passwordpassword", "FeedA")
	userB, cookieB := registerUser(t, base, "feed-b@test.dev", "passwordpassword", "FeedB")

	resp, gBody := request(t, "POST", base+"/v1/groups",
		map[string]any{"name": "FeedTest"}, cookieA)
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	groupID := gBody["id"].(string)
	resp, _ = request(t, "POST", base+"/v1/groups/"+groupID+"/members",
		map[string]any{"email": "feed-b@test.dev"}, cookieA)
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	// Seed: 60 expenses + 20 settlements with strictly increasing incurred_at /
	// settled_at, so the newest-first contract is verifiable. Interleave kinds
	// so the merged feed actually merges (not just concatenates).
	const expenseCount = 60
	const settlementCount = 20
	totalCount := expenseCount + settlementCount
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	expIdx, setIdx := 0, 0
	for i := 0; i < totalCount; i++ {
		at := t0.Add(time.Duration(i) * time.Minute)
		atStr := at.Format(time.RFC3339Nano)
		if (i%4 == 3) && setIdx < settlementCount {
			resp, _ := request(t, "POST", base+"/v1/groups/"+groupID+"/settlements", map[string]any{
				"to_user_id":   userA["id"],
				"amount_cents": 100 + i,
				"settled_at":   atStr,
			}, cookieB)
			require.Equal(t, http.StatusCreated, resp.StatusCode)
			setIdx++
			continue
		}
		if expIdx < expenseCount {
			resp, _ := request(t, "POST", base+"/v1/groups/"+groupID+"/expenses", map[string]any{
				"description":  fmt.Sprintf("E%d", expIdx),
				"amount_cents": 200 + i,
				"payer_id":     userA["id"],
				"mode":         "equal",
				"incurred_at":  atStr,
				"splits": []map[string]any{
					{"user_id": userA["id"]}, {"user_id": userB["id"]},
				},
			}, cookieA)
			require.Equal(t, http.StatusCreated, resp.StatusCode)
			expIdx++
			continue
		}
		resp, _ := request(t, "POST", base+"/v1/groups/"+groupID+"/settlements", map[string]any{
			"to_user_id":   userA["id"],
			"amount_cents": 100 + i,
			"settled_at":   atStr,
		}, cookieB)
		require.Equal(t, http.StatusCreated, resp.StatusCode)
		setIdx++
	}
	require.Equal(t, expenseCount, expIdx)
	require.Equal(t, settlementCount, setIdx)

	// Page 1: default limit 50.
	collected := map[string]bool{}
	var lastAt string
	resp, p1 := request(t, "GET", base+"/v1/groups/"+groupID+"/activity", nil, cookieA)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	items1 := p1["items"].([]any)
	require.Len(t, items1, 50)
	require.NotNil(t, p1["next_cursor"])
	for _, raw := range items1 {
		item := raw.(map[string]any)
		at := item["occurred_at"].(string)
		if lastAt != "" {
			require.True(t, at <= lastAt, "items must be newest first; got %s after %s", at, lastAt)
		}
		lastAt = at
		key := item["kind"].(string) + ":"
		if e, ok := item["expense"].(map[string]any); ok {
			key += e["id"].(string)
		} else if s, ok := item["settlement"].(map[string]any); ok {
			key += s["id"].(string)
		}
		require.False(t, collected[key], "duplicate item across pages: %s", key)
		collected[key] = true
	}

	// Page 2: cursor + limit 25.
	cursor := p1["next_cursor"].(string)
	resp, p2 := request(t, "GET", base+"/v1/groups/"+groupID+"/activity?limit=25&cursor="+cursor, nil, cookieA)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	items2 := p2["items"].([]any)
	require.Len(t, items2, 25)
	require.NotNil(t, p2["next_cursor"])
	for _, raw := range items2 {
		item := raw.(map[string]any)
		at := item["occurred_at"].(string)
		require.True(t, at <= lastAt, "page-2 items must continue strictly newest-first; got %s after %s", at, lastAt)
		lastAt = at
		key := item["kind"].(string) + ":"
		if e, ok := item["expense"].(map[string]any); ok {
			key += e["id"].(string)
		} else if s, ok := item["settlement"].(map[string]any); ok {
			key += s["id"].(string)
		}
		require.False(t, collected[key], "duplicate item across pages: %s", key)
		collected[key] = true
	}

	// Page 3: drains the remaining 5 items, no next_cursor.
	cursor = p2["next_cursor"].(string)
	resp, p3 := request(t, "GET", base+"/v1/groups/"+groupID+"/activity?limit=25&cursor="+cursor, nil, cookieA)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	items3 := p3["items"].([]any)
	require.Len(t, items3, totalCount-50-25)
	require.Nil(t, p3["next_cursor"])
	for _, raw := range items3 {
		item := raw.(map[string]any)
		at := item["occurred_at"].(string)
		require.True(t, at <= lastAt)
		lastAt = at
		key := item["kind"].(string) + ":"
		if e, ok := item["expense"].(map[string]any); ok {
			key += e["id"].(string)
		} else if s, ok := item["settlement"].(map[string]any); ok {
			key += s["id"].(string)
		}
		require.False(t, collected[key], "duplicate item across pages: %s", key)
		collected[key] = true
	}
	require.Len(t, collected, totalCount)

	// Authz: a non-member gets 403.
	_, cookieC := registerUser(t, base, "feed-c@test.dev", "passwordpassword", "FeedC")
	resp, _ = request(t, "GET", base+"/v1/groups/"+groupID+"/activity", nil, cookieC)
	require.Equal(t, http.StatusForbidden, resp.StatusCode)

	// Bad cursor → 400.
	resp, _ = request(t, "GET", base+"/v1/groups/"+groupID+"/activity?cursor=not-a-real-cursor", nil, cookieA)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func netMap(balBody map[string]any) map[string]float64 {
	out := map[string]float64{}
	for _, n := range balBody["net"].([]any) {
		m := n.(map[string]any)
		out[m["user_id"].(string)] = m["net_cents"].(float64)
	}
	return out
}
