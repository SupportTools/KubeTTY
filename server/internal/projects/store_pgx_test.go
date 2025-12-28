package projects

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/pashagolub/pgxmock/v2"
)

// Helper to create a mock pool for testing
func setupMockPool(t *testing.T) (pgxmock.PgxPoolIface, *PGStore) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("failed to create mock pool: %v", err)
	}
	store := NewStoreWithPool(mock, "kubetty-projects")
	return mock, store
}

func TestNewStoreWithPool(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("failed to create mock pool: %v", err)
	}
	defer mock.Close()

	store := NewStoreWithPool(mock, "test-ns")
	if store == nil {
		t.Fatal("expected non-nil store")
	}
	if store.pool == nil {
		t.Fatal("expected non-nil pool in store")
	}
	if store.targetNamespace != "test-ns" {
		t.Errorf("targetNamespace = %s, want test-ns", store.targetNamespace)
	}
}

func TestPGStore_Close(t *testing.T) {
	mock, store := setupMockPool(t)
	mock.ExpectClose()

	store.Close()

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestPGStore_Get_NotFound(t *testing.T) {
	mock, store := setupMockPool(t)
	defer mock.Close()

	id := uuid.New()
	mock.ExpectQuery(`SELECT id, name, display_name`).
		WithArgs(id).
		WillReturnError(pgx.ErrNoRows)

	_, err := store.Get(context.Background(), id)
	if !errors.Is(err, ErrProjectNotFound) {
		t.Errorf("error = %v, want ErrProjectNotFound", err)
	}
}

func TestPGStore_Get_DBError(t *testing.T) {
	mock, store := setupMockPool(t)
	defer mock.Close()

	id := uuid.New()
	expectedErr := errors.New("connection lost")
	mock.ExpectQuery(`SELECT id, name, display_name`).
		WithArgs(id).
		WillReturnError(expectedErr)

	_, err := store.Get(context.Background(), id)
	if err == nil {
		t.Error("expected error, got nil")
	}
}

func TestPGStore_GetByName_NotFound(t *testing.T) {
	mock, store := setupMockPool(t)
	defer mock.Close()

	mock.ExpectQuery(`SELECT id, name, display_name`).
		WithArgs("nonexistent").
		WillReturnError(pgx.ErrNoRows)

	_, err := store.GetByName(context.Background(), "nonexistent")
	if !errors.Is(err, ErrProjectNotFound) {
		t.Errorf("error = %v, want ErrProjectNotFound", err)
	}
}

func TestPGStore_GetByServiceName_NotFound(t *testing.T) {
	mock, store := setupMockPool(t)
	defer mock.Close()

	mock.ExpectQuery(`SELECT id, name, display_name`).
		WithArgs("nonexistent-svc").
		WillReturnError(pgx.ErrNoRows)

	_, err := store.GetByServiceName(context.Background(), "nonexistent-svc")
	if !errors.Is(err, ErrProjectNotFound) {
		t.Errorf("error = %v, want ErrProjectNotFound", err)
	}
}

func TestPGStore_Delete(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mock, store := setupMockPool(t)
		defer mock.Close()

		id := uuid.New()
		mock.ExpectExec(`UPDATE kubetty_projects`).
			WithArgs(id).
			WillReturnResult(pgxmock.NewResult("UPDATE", 1))

		err := store.Delete(context.Background(), id)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("not found", func(t *testing.T) {
		mock, store := setupMockPool(t)
		defer mock.Close()

		id := uuid.New()
		mock.ExpectExec(`UPDATE kubetty_projects`).
			WithArgs(id).
			WillReturnResult(pgxmock.NewResult("UPDATE", 0))

		err := store.Delete(context.Background(), id)
		if !errors.Is(err, ErrProjectNotFound) {
			t.Errorf("error = %v, want ErrProjectNotFound", err)
		}
	})

	t.Run("db error", func(t *testing.T) {
		mock, store := setupMockPool(t)
		defer mock.Close()

		id := uuid.New()
		mock.ExpectExec(`UPDATE kubetty_projects`).
			WithArgs(id).
			WillReturnError(errors.New("db error"))

		err := store.Delete(context.Background(), id)
		if err == nil {
			t.Error("expected error, got nil")
		}
	})
}

func TestPGStore_HardDelete(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mock, store := setupMockPool(t)
		defer mock.Close()

		id := uuid.New()
		mock.ExpectExec(`DELETE FROM kubetty_projects`).
			WithArgs(id).
			WillReturnResult(pgxmock.NewResult("DELETE", 1))

		err := store.HardDelete(context.Background(), id)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("not found", func(t *testing.T) {
		mock, store := setupMockPool(t)
		defer mock.Close()

		id := uuid.New()
		mock.ExpectExec(`DELETE FROM kubetty_projects`).
			WithArgs(id).
			WillReturnResult(pgxmock.NewResult("DELETE", 0))

		err := store.HardDelete(context.Background(), id)
		if !errors.Is(err, ErrProjectNotFound) {
			t.Errorf("error = %v, want ErrProjectNotFound", err)
		}
	})

	t.Run("db error", func(t *testing.T) {
		mock, store := setupMockPool(t)
		defer mock.Close()

		id := uuid.New()
		mock.ExpectExec(`DELETE FROM kubetty_projects`).
			WithArgs(id).
			WillReturnError(errors.New("db error"))

		err := store.HardDelete(context.Background(), id)
		if err == nil {
			t.Error("expected error, got nil")
		}
	})
}

func TestPGStore_SetStatus(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mock, store := setupMockPool(t)
		defer mock.Close()

		id := uuid.New()
		mock.ExpectExec(`UPDATE kubetty_projects`).
			WithArgs(id, StatusRunning, pgxmock.AnyArg()).
			WillReturnResult(pgxmock.NewResult("UPDATE", 1))

		err := store.SetStatus(context.Background(), id, StatusRunning, "healthy")
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("not found", func(t *testing.T) {
		mock, store := setupMockPool(t)
		defer mock.Close()

		id := uuid.New()
		mock.ExpectExec(`UPDATE kubetty_projects`).
			WithArgs(id, StatusFailed, pgxmock.AnyArg()).
			WillReturnResult(pgxmock.NewResult("UPDATE", 0))

		err := store.SetStatus(context.Background(), id, StatusFailed, "")
		if !errors.Is(err, ErrProjectNotFound) {
			t.Errorf("error = %v, want ErrProjectNotFound", err)
		}
	})

	t.Run("db error", func(t *testing.T) {
		mock, store := setupMockPool(t)
		defer mock.Close()

		id := uuid.New()
		mock.ExpectExec(`UPDATE kubetty_projects`).
			WithArgs(id, StatusFailed, pgxmock.AnyArg()).
			WillReturnError(errors.New("db error"))

		err := store.SetStatus(context.Background(), id, StatusFailed, "error")
		if err == nil {
			t.Error("expected error, got nil")
		}
	})
}

func TestPGStore_SetPaused(t *testing.T) {
	t.Run("pause success", func(t *testing.T) {
		mock, store := setupMockPool(t)
		defer mock.Close()

		id := uuid.New()
		mock.ExpectExec(`UPDATE kubetty_projects`).
			WithArgs(id, true).
			WillReturnResult(pgxmock.NewResult("UPDATE", 1))

		err := store.SetPaused(context.Background(), id, true)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("unpause success", func(t *testing.T) {
		mock, store := setupMockPool(t)
		defer mock.Close()

		id := uuid.New()
		mock.ExpectExec(`UPDATE kubetty_projects`).
			WithArgs(id, false).
			WillReturnResult(pgxmock.NewResult("UPDATE", 1))

		err := store.SetPaused(context.Background(), id, false)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("not found", func(t *testing.T) {
		mock, store := setupMockPool(t)
		defer mock.Close()

		id := uuid.New()
		mock.ExpectExec(`UPDATE kubetty_projects`).
			WithArgs(id, true).
			WillReturnResult(pgxmock.NewResult("UPDATE", 0))

		err := store.SetPaused(context.Background(), id, true)
		if !errors.Is(err, ErrProjectNotFound) {
			t.Errorf("error = %v, want ErrProjectNotFound", err)
		}
	})
}

func TestPGStore_UpdateHealthCheck(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mock, store := setupMockPool(t)
		defer mock.Close()

		id := uuid.New()
		mock.ExpectExec(`UPDATE kubetty_projects`).
			WithArgs(id, pgxmock.AnyArg()).
			WillReturnResult(pgxmock.NewResult("UPDATE", 1))

		err := store.UpdateHealthCheck(context.Background(), id, "10.0.0.1")
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("not found", func(t *testing.T) {
		mock, store := setupMockPool(t)
		defer mock.Close()

		id := uuid.New()
		mock.ExpectExec(`UPDATE kubetty_projects`).
			WithArgs(id, pgxmock.AnyArg()).
			WillReturnResult(pgxmock.NewResult("UPDATE", 0))

		err := store.UpdateHealthCheck(context.Background(), id, "")
		if !errors.Is(err, ErrProjectNotFound) {
			t.Errorf("error = %v, want ErrProjectNotFound", err)
		}
	})

	t.Run("db error", func(t *testing.T) {
		mock, store := setupMockPool(t)
		defer mock.Close()

		id := uuid.New()
		mock.ExpectExec(`UPDATE kubetty_projects`).
			WithArgs(id, pgxmock.AnyArg()).
			WillReturnError(errors.New("db error"))

		err := store.UpdateHealthCheck(context.Background(), id, "10.0.0.1")
		if err == nil {
			t.Error("expected error, got nil")
		}
	})
}

func TestPGStore_UpdateLastActivity(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mock, store := setupMockPool(t)
		defer mock.Close()

		mock.ExpectExec(`UPDATE kubetty_projects`).
			WithArgs("my-project").
			WillReturnResult(pgxmock.NewResult("UPDATE", 1))

		err := store.UpdateLastActivity(context.Background(), "my-project")
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("not found", func(t *testing.T) {
		mock, store := setupMockPool(t)
		defer mock.Close()

		mock.ExpectExec(`UPDATE kubetty_projects`).
			WithArgs("nonexistent").
			WillReturnResult(pgxmock.NewResult("UPDATE", 0))

		err := store.UpdateLastActivity(context.Background(), "nonexistent")
		if !errors.Is(err, ErrProjectNotFound) {
			t.Errorf("error = %v, want ErrProjectNotFound", err)
		}
	})
}

func TestPGStore_GetStatusCounts(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mock, store := setupMockPool(t)
		defer mock.Close()

		mock.ExpectQuery(`SELECT status, COUNT`).
			WillReturnRows(pgxmock.NewRows([]string{"status", "count"}).
				AddRow("running", 5).
				AddRow("pending", 2).
				AddRow("failed", 1))

		counts, err := store.GetStatusCounts(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if counts[StatusRunning] != 5 {
			t.Errorf("running count = %d, want 5", counts[StatusRunning])
		}
		if counts[StatusPending] != 2 {
			t.Errorf("pending count = %d, want 2", counts[StatusPending])
		}
		if counts[StatusFailed] != 1 {
			t.Errorf("failed count = %d, want 1", counts[StatusFailed])
		}
	})

	t.Run("db error", func(t *testing.T) {
		mock, store := setupMockPool(t)
		defer mock.Close()

		mock.ExpectQuery(`SELECT status, COUNT`).
			WillReturnError(errors.New("db error"))

		_, err := store.GetStatusCounts(context.Background())
		if err == nil {
			t.Error("expected error, got nil")
		}
	})
}

func TestPGStore_List_DBError(t *testing.T) {
	mock, store := setupMockPool(t)
	defer mock.Close()

	mock.ExpectQuery(`SELECT id, name, display_name`).
		WillReturnError(errors.New("db error"))

	_, err := store.List(context.Background(), ListFilter{})
	if err == nil {
		t.Error("expected error, got nil")
	}
}

func TestPGStore_ListByStatuses_EmptyStatuses(t *testing.T) {
	mock, store := setupMockPool(t)
	defer mock.Close()

	projects, err := store.ListByStatuses(context.Background(), []ProjectStatus{})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(projects) != 0 {
		t.Errorf("len(projects) = %d, want 0", len(projects))
	}
}

func TestPGStore_ListByStatuses_DBError(t *testing.T) {
	mock, store := setupMockPool(t)
	defer mock.Close()

	mock.ExpectQuery(`SELECT id, name, display_name`).
		WithArgs([]string{"pending"}).
		WillReturnError(errors.New("db error"))

	_, err := store.ListByStatuses(context.Background(), []ProjectStatus{StatusPending})
	if err == nil {
		t.Error("expected error, got nil")
	}
}

func TestPGStore_GetRecentlyFailed_DBError(t *testing.T) {
	mock, store := setupMockPool(t)
	defer mock.Close()

	since := time.Now().Add(-time.Hour)
	mock.ExpectQuery(`SELECT id, name, display_name`).
		WithArgs(since, 10).
		WillReturnError(errors.New("db error"))

	_, err := store.GetRecentlyFailed(context.Background(), since, 10)
	if err == nil {
		t.Error("expected error, got nil")
	}
}

func TestPGStore_Create_InvalidName(t *testing.T) {
	mock, store := setupMockPool(t)
	defer mock.Close()

	// Invalid names that don't match DNS-1123 pattern
	invalidNames := []string{"My Project", "project!", "PROJECT", "-start", "end-"}

	for _, name := range invalidNames {
		_, err := store.Create(context.Background(), CreateProjectRequest{Name: name})
		if !errors.Is(err, ErrInvalidName) {
			t.Errorf("Create with name %q: error = %v, want ErrInvalidName", name, err)
		}
	}
}
