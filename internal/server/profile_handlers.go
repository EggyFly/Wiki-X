package server

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/EggyFly/Wiki-X/internal/store"
)

type userProfileData struct {
	baseData
	Profile *store.User
}

func (a *App) handleUserProfile(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.PathValue("name"))
	if name == "" {
		http.NotFound(w, r)
		return
	}
	user, err := a.store.UserByName(r.Context(), name)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		slog.Error("load user profile failed", "err", err, "name", name)
		a.renderMessage(w, r, http.StatusInternalServerError, "读取失败", "暂时无法读取用户主页.")
		return
	}
	if user.Disabled {
		http.NotFound(w, r)
		return
	}
	a.renderUI(w, r, http.StatusOK, "user_profile", userProfileData{
		baseData: a.baseData(r, user.Name),
		Profile:  user,
	})
}
