package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

const scenePlaylistSyncInterval = 5 * time.Minute

func displayScenePlaylistName(scene audioMuseClusterPlaylist) string {
	name := strings.TrimSpace(scene.Name)
	if name == "" {
		return ""
	}
	label := "Scene: " + name
	if subtitle := strings.TrimSpace(scene.Subtitle); subtitle != "" {
		label += " | " + subtitle
	}
	return label
}

func buildScenePlaylistRenames(existing []navidromePlaylist, scenes []audioMuseClusterPlaylist) map[string]string {
	if len(existing) == 0 || len(scenes) == 0 {
		return nil
	}
	byName := make(map[string][]navidromePlaylist, len(existing))
	for _, playlist := range existing {
		name := strings.TrimSpace(playlist.Name)
		if name == "" {
			continue
		}
		byName[name] = append(byName[name], playlist)
	}
	renames := make(map[string]string)
	for _, scene := range scenes {
		rawKey := strings.TrimSpace(scene.Key)
		if rawKey == "" {
			continue
		}
		desiredName := displayScenePlaylistName(scene)
		if desiredName == "" || desiredName == rawKey {
			continue
		}
		for _, playlist := range byName[rawKey] {
			if strings.TrimSpace(playlist.ID) == "" {
				continue
			}
			renames[playlist.ID] = desiredName
		}
	}
	if len(renames) == 0 {
		return nil
	}
	return renames
}

func syncScenePlaylistsInNavidrome(ctx context.Context, scenes []audioMuseClusterPlaylist) (int, error) {
	if len(scenes) == 0 {
		return 0, nil
	}
	client, err := newNavidromeClientFromEnv()
	if err != nil {
		return 0, err
	}
	playlists, err := client.GetPlaylists(ctx)
	if err != nil {
		return 0, err
	}
	renames := buildScenePlaylistRenames(playlists, scenes)
	if len(renames) == 0 {
		return 0, nil
	}
	ids := make([]string, 0, len(renames))
	for id := range renames {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	renamed := 0
	for _, id := range ids {
		if err := client.UpdatePlaylistName(ctx, id, renames[id]); err != nil {
			return renamed, err
		}
		renamed++
	}
	return renamed, nil
}

func startScenePlaylistSyncManager(ctx context.Context, publish func(string)) {
	if !sonicAnalysisEnabled() {
		return
	}
	runPass := func() {
		passCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
		defer cancel()

		client := newAudioMuseListenClient()
		if client == nil {
			return
		}
		scenes, err := client.playlists(passCtx)
		if err != nil || len(scenes) == 0 {
			if err != nil {
				log.Warn().Err(err).Msg("Scene playlist sync skipped")
			}
			return
		}
		renamed, err := syncScenePlaylistsInNavidrome(passCtx, scenes)
		if err != nil {
			log.Warn().Err(err).Msg("Scene playlist sync failed")
			return
		}
		if renamed > 0 {
			message := fmt.Sprintf("Normalized %d sonic scene playlist name(s).", renamed)
			log.Info().Int("renamed", renamed).Msg("Scene playlist sync complete")
			if publish != nil {
				publish(message)
			}
		}
	}

	runPass()

	ticker := time.NewTicker(scenePlaylistSyncInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runPass()
		}
	}
}
