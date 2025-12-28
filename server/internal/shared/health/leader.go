// Package health provides health check handlers for KubeTTY server components.
package health

import (
	"net/http"

	"github.com/supporttools/KubeTTY/server/internal/shared/util"
)

// LeaderInfo provides leader election status information.
type LeaderInfo interface {
	IsLeader() bool
	GetCurrentLeader() string
	GetIdentity() string
}

// LeaderStatusResponse is the JSON response for leader status.
type LeaderStatusResponse struct {
	IsLeader      bool   `json:"isLeader"`
	CurrentLeader string `json:"currentLeader"`
	Identity      string `json:"identity"`
	Status        string `json:"status"`
}

// NewLeaderStatusHandler creates an HTTP handler that returns leader election status.
// If leaderInfo is nil, it returns a response indicating leader election is disabled.
func NewLeaderStatusHandler(leaderInfo LeaderInfo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if leaderInfo == nil {
			response := LeaderStatusResponse{
				IsLeader:      true, // If no leader election, this instance acts as leader
				CurrentLeader: "n/a",
				Identity:      "n/a",
				Status:        "disabled",
			}
			_ = util.WriteJSON(w, http.StatusOK, response)
			return
		}

		response := LeaderStatusResponse{
			IsLeader:      leaderInfo.IsLeader(),
			CurrentLeader: leaderInfo.GetCurrentLeader(),
			Identity:      leaderInfo.GetIdentity(),
			Status:        "enabled",
		}

		_ = util.WriteJSON(w, http.StatusOK, response)
	}
}
