package httpserver

import (
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"
)

type backupFile struct {
	Name    string
	ModTime time.Time
}

type settingsPageData struct {
	pageData
	AutoIssue bool
	Backups   []backupFile
	BackupErr string
}

func listBackups(dir string) ([]backupFile, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := make([]backupFile, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, backupFile{Name: e.Name(), ModTime: info.ModTime()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ModTime.After(out[j].ModTime) })
	return out, nil
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	autoIssue, err := s.svc.AutoIssueEnabled(r.Context())
	if err != nil {
		s.log.Error("get auto_issue setting", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	backups, err := listBackups(s.cfg.BackupDir)
	backupErr := ""
	if err != nil {
		s.log.Error("list backups", "error", err, "dir", s.cfg.BackupDir)
		backupErr = "Не удалось прочитать каталог бэкапов: " + filepath.Clean(s.cfg.BackupDir)
	}

	s.render(w, r, "settings.html", settingsPageData{
		pageData:  s.newPageData("settings"),
		AutoIssue: autoIssue,
		Backups:   backups,
		BackupErr: backupErr,
	})
}

func (s *Server) handleSetAutoIssue(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	enabled := r.FormValue("auto_issue") == "on"

	if err := s.svc.SetAutoIssue(r.Context(), enabled); err != nil {
		s.log.Error("set auto_issue setting", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, s.base()+"/settings", http.StatusSeeOther)
}
