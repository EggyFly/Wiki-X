package server

import (
	"log/slog"
	"net/http"
	"net/url"

	"github.com/EggyFly/Wiki-X/internal/auth"
	"github.com/EggyFly/Wiki-X/internal/store"
)

func (a *App) handleTranslations(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireRight(w, r, auth.RightEdit); !ok {
		return
	}
	page, size := adminPage(r, 50)
	groups, total, err := a.store.ListTranslationGroups(r.Context(), size, 0)
	if err != nil {
		slog.Error("list translation groups failed", "err", err)
		a.renderMessage(w, r, http.StatusInternalServerError, "读取失败", "暂时无法读取翻译分组.")
		return
	}
	pager := newPager(page, totalPages(total, size), size)
	if pager.Page > 1 {
		groups, total, err = a.store.ListTranslationGroups(r.Context(), size, (pager.Page-1)*size)
		if err != nil {
			slog.Error("list translation groups failed", "err", err)
			a.renderMessage(w, r, http.StatusInternalServerError, "读取失败", "暂时无法读取翻译分组.")
			return
		}
		pager = newPager(pager.Page, totalPages(total, size), size)
	}

	locales := a.cfg.Site.LocaleConfigs()
	defaultCode := a.cfg.Site.Language
	rows := make([]translationRow, 0, len(groups))
	for _, group := range groups {
		byLocale := make(map[string]*store.TranslationPage)
		for _, item := range group.Pages {
			code := a.site.LocaleCodeForRoute(item.Route)
			if _, exists := byLocale[code]; !exists {
				byLocale[code] = item
			}
		}
		defaultPage := byLocale[defaultCode]
		row := translationRow{Key: group.Key, DefaultMissing: defaultPage == nil}
		if defaultPage != nil {
			row.DefaultTitle = defaultPage.Title
			row.DefaultRoute = defaultPage.Route
		}
		for _, locale := range locales {
			cell := translationLocaleCell{Code: locale.Code, Label: locale.Label}
			item := byLocale[locale.Code]
			if item == nil {
				cell.Missing = true
				if defaultPage != nil {
					baseRoute := a.site.BaseRouteFor(defaultPage.Route)
					cell.Route = a.site.LocaleRoute(locale.Prefix, baseRoute)
					cell.EditURL = "/edit?route=" + url.QueryEscape(cell.Route) + "&translation_key=" + url.QueryEscape(group.Key)
				}
			} else {
				cell.Route = item.Route
				cell.Title = item.Title
				cell.EditURL = "/edit?route=" + url.QueryEscape(item.Route)
				if locale.Code != defaultCode {
					if item.TranslationBaseVersion == "" {
						cell.Unknown = true
					} else if defaultPage != nil && item.TranslationBaseVersion != defaultPage.Version {
						cell.Outdated = true
					}
				}
			}
			row.Locales = append(row.Locales, cell)
		}
		rows = append(rows, row)
	}
	data := translationsData{
		baseData: a.baseData(r, "翻译管理"),
		Rows:     rows,
		Pager:    pager,
	}
	a.renderUI(w, r, http.StatusOK, "translations", data)
}
