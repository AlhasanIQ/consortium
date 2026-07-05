package storage

import (
	"errors"
	"testing"
	"time"
)

func TestOpenAIObjectStoreRoundTripResponseWithItems(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	now := time.Now().UTC()
	obj := &OpenAIObjectRecord{
		ID:             "resp-test",
		ObjectType:     OpenAIObjectTypeResponse,
		KeyID:          "key-1",
		UserID:         "system",
		Endpoint:       "/v1/responses",
		RequestedModel: "consortium-default",
		Status:         OpenAIObjectStatusCompleted,
		Store:          true,
		MetadataJSON:   `{"tenant":"acme"}`,
		ResponseJSON:   `{"id":"resp-test","object":"response","status":"completed"}`,
		UsageJSON:      `{"total_tokens":10}`,
		CreatedAt:      now,
		UpdatedAt:      now,
		CompletedAt:    &now,
	}
	if err := store.CreateOpenAIObject(obj); err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceOpenAIObjectItems("resp-test", "key-1", []OpenAIObjectItem{
		{ID: "item-in", ObjectID: "resp-test", ItemKind: OpenAIItemKindInput, ItemIndex: 0, OpenAIItemID: "msg-in", Role: "user", ContentJSON: `{"text":"hello"}`, RawJSON: `{"role":"user","content":"hello"}`},
		{ID: "item-out", ObjectID: "resp-test", ItemKind: OpenAIItemKindOutput, ItemIndex: 0, OpenAIItemID: "msg-out", Role: "assistant", ContentJSON: `{"text":"hi"}`, RawJSON: `{"role":"assistant","content":"hi"}`},
	}); err != nil {
		t.Fatal(err)
	}

	got, err := store.GetOpenAIObject("resp-test", "key-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != obj.ID || got.ObjectType != obj.ObjectType || got.ResponseJSON == "" || got.CompletedAt == nil {
		t.Fatalf("object mismatch: %+v", got)
	}

	items, page, err := store.ListOpenAIObjectItems("resp-test", "key-1", OpenAIItemKindInput, OpenAIListPageRequest{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].OpenAIItemID != "msg-in" {
		t.Fatalf("items = %+v", items)
	}
	if page.HasMore || page.FirstID != "item-in" || page.LastID != "item-in" {
		t.Fatalf("page = %+v", page)
	}
}

func TestOpenAIObjectStoreScopesObjectsAndItemsByKey(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	now := time.Now().UTC()
	obj := &OpenAIObjectRecord{
		ID:           "resp-private",
		ObjectType:   OpenAIObjectTypeResponse,
		KeyID:        "key-a",
		UserID:       "system",
		Endpoint:     "/v1/responses",
		Status:       OpenAIObjectStatusCompleted,
		Store:        true,
		ResponseJSON: `{"id":"resp-private"}`,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := store.CreateOpenAIObject(obj); err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceOpenAIObjectItems("resp-private", "key-a", []OpenAIObjectItem{
		{ID: "item-private", ObjectID: "resp-private", ItemKind: OpenAIItemKindInput, ItemIndex: 0, OpenAIItemID: "msg-private", Role: "user", ContentJSON: `{}`, RawJSON: `{}`},
	}); err != nil {
		t.Fatal(err)
	}

	if err := store.ReplaceOpenAIObjectItems("resp-private", "key-b", []OpenAIObjectItem{
		{ID: "item-replaced", ObjectID: "resp-private", ItemKind: OpenAIItemKindInput, ItemIndex: 0, OpenAIItemID: "msg-replaced", Role: "user", ContentJSON: `{}`, RawJSON: `{}`},
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ReplaceOpenAIObjectItems as other key err = %v, want ErrNotFound", err)
	}
	if _, err := store.GetOpenAIObject("resp-private", "key-b"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetOpenAIObject as other key err = %v, want ErrNotFound", err)
	}
	if _, _, err := store.ListOpenAIObjectItems("resp-private", "key-b", OpenAIItemKindInput, OpenAIListPageRequest{Limit: 10}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ListOpenAIObjectItems as other key err = %v, want ErrNotFound", err)
	}
	items, _, err := store.ListOpenAIObjectItems("resp-private", "key-a", OpenAIItemKindInput, OpenAIListPageRequest{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].OpenAIItemID != "msg-private" {
		t.Fatalf("items after rejected replace = %+v", items)
	}
}

func TestOpenAIObjectStoreListsObjectsWithCursorPagination(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	base := time.Now().UTC().Add(-time.Hour)
	for i, id := range []string{"chat-1", "chat-2", "chat-3"} {
		at := base.Add(time.Duration(i) * time.Minute)
		if err := store.CreateOpenAIObject(&OpenAIObjectRecord{
			ID:           id,
			ObjectType:   OpenAIObjectTypeChatCompletion,
			KeyID:        "key-1",
			UserID:       "system",
			Endpoint:     "/v1/chat/completions",
			Status:       OpenAIObjectStatusCompleted,
			Store:        true,
			ResponseJSON: `{"id":"` + id + `"}`,
			CreatedAt:    at,
			UpdatedAt:    at,
		}); err != nil {
			t.Fatal(err)
		}
	}

	first, page, err := store.ListOpenAIObjects(OpenAIObjectListFilters{
		KeyID:      "key-1",
		ObjectType: OpenAIObjectTypeChatCompletion,
		Limit:      2,
		Order:      OpenAIListOrderDesc,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 2 || first[0].ID != "chat-3" || first[1].ID != "chat-2" {
		t.Fatalf("first page = %+v", first)
	}
	if !page.HasMore || page.FirstID != "chat-3" || page.LastID != "chat-2" {
		t.Fatalf("first page metadata = %+v", page)
	}

	second, page, err := store.ListOpenAIObjects(OpenAIObjectListFilters{
		KeyID:      "key-1",
		ObjectType: OpenAIObjectTypeChatCompletion,
		Limit:      2,
		After:      page.LastID,
		Order:      OpenAIListOrderDesc,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 1 || second[0].ID != "chat-1" {
		t.Fatalf("second page = %+v", second)
	}
	if page.HasMore || page.FirstID != "chat-1" || page.LastID != "chat-1" {
		t.Fatalf("second page metadata = %+v", page)
	}
}

func TestOpenAIObjectStoreDoesNotOverwriteCancelledObject(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	now := time.Now().UTC()
	if err := store.CreateOpenAIObject(&OpenAIObjectRecord{
		ID:           "resp-cancelled",
		ObjectType:   OpenAIObjectTypeResponse,
		KeyID:        "key-1",
		UserID:       "system",
		Endpoint:     "/v1/responses",
		Status:       OpenAIObjectStatusInProgress,
		Store:        true,
		ResponseJSON: `{"id":"resp-cancelled","status":"in_progress"}`,
		CreatedAt:    now,
		UpdatedAt:    now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateOpenAIObjectCompletion("resp-cancelled", "key-1", OpenAIObjectCompletion{
		Status:       OpenAIObjectStatusCancelled,
		ResponseJSON: `{"id":"resp-cancelled","status":"cancelled"}`,
		ErrorCode:    "cancelled",
		ErrorMessage: "cancelled",
		CompletedAt:  now,
	}); err != nil {
		t.Fatalf("cancel object: %v", err)
	}
	if err := store.UpdateOpenAIObjectCompletion("resp-cancelled", "key-1", OpenAIObjectCompletion{
		Status:       OpenAIObjectStatusCompleted,
		ResponseJSON: `{"id":"resp-cancelled","status":"completed"}`,
		CompletedAt:  now,
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("complete cancelled object err = %v, want ErrNotFound", err)
	}
	got, err := store.GetOpenAIObject("resp-cancelled", "key-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != OpenAIObjectStatusCancelled || got.ResponseJSON != `{"id":"resp-cancelled","status":"cancelled"}` {
		t.Fatalf("cancelled object was overwritten: %+v", got)
	}
}

func TestCreateOpenAIObjectWithItemsRollsBackWhenItemsFail(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	now := time.Now().UTC()
	err = store.CreateOpenAIObjectWithItems(&OpenAIObjectRecord{
		ID:           "resp-atomic-create",
		ObjectType:   OpenAIObjectTypeResponse,
		KeyID:        "key-1",
		UserID:       "system",
		Endpoint:     "/v1/responses",
		Status:       OpenAIObjectStatusInProgress,
		Store:        true,
		Background:   true,
		ResponseJSON: `{"id":"resp-atomic-create","status":"in_progress"}`,
		CreatedAt:    now,
		UpdatedAt:    now,
	}, []OpenAIObjectItem{
		{ID: "bad-item", ObjectID: "other-object", ItemKind: OpenAIItemKindInput, ItemIndex: 0, ContentJSON: `{}`, RawJSON: `{}`},
	})
	if err == nil {
		t.Fatal("CreateOpenAIObjectWithItems succeeded with an item for a different object")
	}
	if _, getErr := store.GetOpenAIObject("resp-atomic-create", "key-1"); !errors.Is(getErr, ErrNotFound) {
		t.Fatalf("GetOpenAIObject after failed atomic create err = %v, want ErrNotFound", getErr)
	}
}

func TestCompleteOpenAIObjectWithItemsUsageAndIdempotencyIsAtomic(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	now := time.Now().UTC()
	if err := store.CreateOpenAIObjectWithItems(&OpenAIObjectRecord{
		ID:           "resp-atomic-complete",
		ObjectType:   OpenAIObjectTypeResponse,
		KeyID:        "key-1",
		UserID:       "system",
		Endpoint:     "/v1/responses",
		Status:       OpenAIObjectStatusInProgress,
		Store:        true,
		Background:   true,
		ResponseJSON: `{"id":"resp-atomic-complete","status":"in_progress"}`,
		CreatedAt:    now,
		UpdatedAt:    now,
	}, []OpenAIObjectItem{
		{ID: "input-original", ObjectID: "resp-atomic-complete", ItemKind: OpenAIItemKindInput, ItemIndex: 0, ContentJSON: `{"text":"hello"}`, RawJSON: `{"role":"user","content":"hello"}`},
	}); err != nil {
		t.Fatalf("CreateOpenAIObjectWithItems: %v", err)
	}
	if err := store.CreateAPIUsage(&APIUsageRecord{
		ID:         "usage-atomic",
		RequestID:  "req-atomic",
		KeyID:      "key-1",
		UserID:     "system",
		Endpoint:   "/v1/responses",
		Status:     APIUsageStatusRunning,
		HTTPStatus: 200,
		CreatedAt:  now,
	}); err != nil {
		t.Fatalf("CreateAPIUsage: %v", err)
	}
	_, _, err = store.ReserveAPIIdempotency(&APIIdempotencyRecord{
		ID:                 "idem-atomic",
		KeyID:              "key-1",
		IdempotencyKey:     "idem-key",
		RequestFingerprint: "fingerprint",
		CreatedAt:          now,
		ExpiresAt:          now.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("ReserveAPIIdempotency: %v", err)
	}

	err = store.CompleteOpenAIObjectWithItemsUsageAndIdempotency(
		"resp-atomic-complete",
		"key-1",
		OpenAIObjectCompletion{
			JobID:        "job-atomic",
			Status:       OpenAIObjectStatusCompleted,
			ResponseJSON: `{"id":"resp-atomic-complete","status":"completed"}`,
			UsageJSON:    `{"total_tokens":3}`,
			CompletedAt:  now.Add(time.Second),
		},
		[]OpenAIObjectItem{
			{ID: "bad-output", ObjectID: "other-object", ItemKind: OpenAIItemKindOutput, ItemIndex: 0, ContentJSON: `{}`, RawJSON: `{}`},
		},
		"usage-atomic",
		APIUsageCompletion{
			JobID:        "job-atomic",
			Status:       APIUsageStatusSucceeded,
			HTTPStatus:   200,
			TokensInput:  1,
			TokensOutput: 2,
			TokensTotal:  3,
			CompletedAt:  now.Add(time.Second),
		},
		"idem-atomic",
		"idem-key",
		`{"id":"resp-atomic-complete","status":"completed"}`,
		200,
	)
	if err == nil {
		t.Fatal("CompleteOpenAIObjectWithItemsUsageAndIdempotency succeeded with an item for a different object")
	}

	obj, err := store.GetOpenAIObject("resp-atomic-complete", "key-1")
	if err != nil {
		t.Fatal(err)
	}
	if obj.Status != OpenAIObjectStatusInProgress || obj.JobID != "" || obj.ResponseJSON != `{"id":"resp-atomic-complete","status":"in_progress"}` {
		t.Fatalf("object changed after failed atomic completion: %+v", obj)
	}
	items, _, err := store.ListOpenAIObjectItems("resp-atomic-complete", "key-1", OpenAIItemKindInput, OpenAIListPageRequest{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != "input-original" {
		t.Fatalf("input items changed after failed atomic completion: %+v", items)
	}
	usages, err := store.ListAPIUsage(APIUsageFilters{KeyID: "key-1", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(usages) != 1 || usages[0].Status != APIUsageStatusRunning || usages[0].JobID != "" {
		t.Fatalf("usage changed after failed atomic completion: %+v", usages)
	}
	idem, err := store.GetAPIIdempotency("key-1", "idem-key")
	if err != nil {
		t.Fatal(err)
	}
	if idem.ResponseBody != "" || idem.HTTPStatus != 0 {
		t.Fatalf("idempotency changed after failed atomic completion: %+v", idem)
	}
}
