package main

import (
	"embed"
	"html/template"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed ui/*
var uiFiles embed.FS

type uiPageData struct {
	Title       string
	Subtitle    string
	InitialView string
}

var indexTemplate = template.Must(template.ParseFS(uiFiles, "ui/index.html"))

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	data := uiPageData{
		Title:       "Groovarr",
		Subtitle:    "Mobile-first command center for library chat, events, and recommendations.",
		InitialView: "chat",
	}
	if strings.HasPrefix(r.URL.Path, "/listen") {
		data.InitialView = "listen"
	} else if strings.HasPrefix(r.URL.Path, "/explore") {
		data.InitialView = "explore"
	}
	if err := indexTemplate.Execute(w, data); err != nil {
		http.Error(w, "failed to render page", http.StatusInternalServerError)
	}
}

func (s *Server) staticUIHandler() http.Handler {
	sub, err := fs.Sub(uiFiles, "ui")
	if err != nil {
		panic(err)
	}
	return http.FileServer(http.FS(sub))
}
