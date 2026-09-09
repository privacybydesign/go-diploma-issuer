package main

import (
	"testing"
	"time"

	"go-diploma-issuer/diploma"

	"github.com/stretchr/testify/require"
)

func TestInMemorySessionStorageRoundTrip(t *testing.T) {
	storage := NewInMemorySessionStorage()

	session := &Session{
		Id:        "abc",
		CreatedAt: time.Now(),
		Stage:     StageValidated,
		Documents: []*diploma.Document{testDiploma(), testSecondDiploma()},
	}
	require.NoError(t, storage.Store(session))

	got, err := storage.Retrieve("abc")
	require.NoError(t, err)
	require.Equal(t, session.Id, got.Id)
	require.Equal(t, StageValidated, got.Stage)
	require.Len(t, got.Documents, 2)
	require.Equal(t, "319221", got.Documents[0].DocumentNumber)
	require.Equal(t, "2896311", got.Documents[1].DocumentNumber)

	// The stored copy is independent from the caller's struct.
	session.Stage = StageDisclosing
	got, err = storage.Retrieve("abc")
	require.NoError(t, err)
	require.Equal(t, StageValidated, got.Stage)

	// Updating replaces.
	session.IrmaToken = "token"
	require.NoError(t, storage.Store(session))
	got, err = storage.Retrieve("abc")
	require.NoError(t, err)
	require.Equal(t, StageDisclosing, got.Stage)
	require.Equal(t, "token", got.IrmaToken)

	require.NoError(t, storage.Remove("abc"))
	_, err = storage.Retrieve("abc")
	require.Error(t, err)
	require.Error(t, storage.Remove("abc"))
}

func TestInMemorySessionStorageExpiry(t *testing.T) {
	storage := NewInMemorySessionStorage()
	now := time.Now()
	storage.now = func() time.Time { return now }

	require.NoError(t, storage.Store(&Session{Id: "old", CreatedAt: now}))

	now = now.Add(SessionLifetime + time.Second)
	_, err := storage.Retrieve("old")
	require.Error(t, err, "expired sessions are not returned")
}
