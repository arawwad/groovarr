package main

import "testing"

func TestSonicAnalysisEnabledDefaultsTrue(t *testing.T) {
	t.Setenv("SONIC_ANALYSIS_ENABLED", "")
	if !sonicAnalysisEnabled() {
		t.Fatal("sonicAnalysisEnabled() = false, want true by default")
	}
}

func TestSonicAnalysisEnabledFalseValues(t *testing.T) {
	for _, raw := range []string{"0", "false", "FALSE", "no", "off"} {
		t.Run(raw, func(t *testing.T) {
			t.Setenv("SONIC_ANALYSIS_ENABLED", raw)
			if sonicAnalysisEnabled() {
				t.Fatalf("sonicAnalysisEnabled() = true for %q, want false", raw)
			}
		})
	}
}

func TestSonicAnalysisEnabledTreatsOtherValuesAsTrue(t *testing.T) {
	for _, raw := range []string{"1", "true", "yes", "internal-only"} {
		t.Run(raw, func(t *testing.T) {
			t.Setenv("SONIC_ANALYSIS_ENABLED", raw)
			if !sonicAnalysisEnabled() {
				t.Fatalf("sonicAnalysisEnabled() = false for %q, want true", raw)
			}
		})
	}
}

func TestFormatClusterSceneName(t *testing.T) {
	tests := []struct {
		raw          string
		wantName     string
		wantSubtitle string
	}{
		{
			raw:          "Indie_Rock_Alternative_Medium_Relaxed_Sad_automatic",
			wantName:     "Indie / Rock / Alternative • Mid-Tempo",
			wantSubtitle: "Relaxed, Sad",
		},
		{
			raw:          "Electronic_Pop_Indie_Fast_Danceable_Party",
			wantName:     "Electronic / Pop / Indie • Fast Tempo",
			wantSubtitle: "Danceable, Party",
		},
		{
			raw:          "Night Drift",
			wantName:     "Night Drift",
			wantSubtitle: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			gotName, gotSubtitle := formatClusterSceneName(tt.raw)
			if gotName != tt.wantName || gotSubtitle != tt.wantSubtitle {
				t.Fatalf("formatClusterSceneName(%q) = (%q, %q), want (%q, %q)", tt.raw, gotName, gotSubtitle, tt.wantName, tt.wantSubtitle)
			}
		})
	}
}

func TestDisplayScenePlaylistName(t *testing.T) {
	scene := audioMuseClusterPlaylist{
		Name:     "Indie / Rock / Alternative • Mid-Tempo",
		Subtitle: "Relaxed, Sad",
	}
	if got := displayScenePlaylistName(scene); got != "Scene: Indie / Rock / Alternative • Mid-Tempo | Relaxed, Sad" {
		t.Fatalf("displayScenePlaylistName() = %q", got)
	}
}

func TestBuildScenePlaylistRenames(t *testing.T) {
	existing := []navidromePlaylist{
		{ID: "scene-1", Name: "Indie_Rock_Alternative_Medium_Relaxed_Sad_automatic"},
		{ID: "scene-2", Name: "Already Clean"},
	}
	scenes := []audioMuseClusterPlaylist{
		{
			Key:      "Indie_Rock_Alternative_Medium_Relaxed_Sad_automatic",
			Name:     "Indie / Rock / Alternative • Mid-Tempo",
			Subtitle: "Relaxed, Sad",
		},
	}
	renames := buildScenePlaylistRenames(existing, scenes)
	if len(renames) != 1 {
		t.Fatalf("len(buildScenePlaylistRenames()) = %d, want 1", len(renames))
	}
	if got := renames["scene-1"]; got != "Scene: Indie / Rock / Alternative • Mid-Tempo | Relaxed, Sad" {
		t.Fatalf("rename target = %q", got)
	}
}
