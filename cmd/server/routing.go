package main

import (
	"context"
	"groovarr/internal/agent"
	"strings"
)

func (s *Server) tryDeterministicRoute(ctx context.Context, msg string, history []agent.Message) (ChatResponse, bool) {
	q := strings.ToLower(strings.TrimSpace(msg))
	if q == "" {
		return ChatResponse{}, false
	}

	if resp, ok := s.tryDeterministicSavedPlaylistTracksQuery(ctx, msg, history); ok {
		return ChatResponse{Response: resp}, true
	}
	if resp, ok := s.tryDeterministicSavedPlaylistFollowUp(ctx, msg, history); ok {
		return ChatResponse{Response: resp}, true
	}
	if resp, ok := s.tryDeterministicSceneClarification(ctx, msg); ok {
		return ChatResponse{Response: resp}, true
	}
	if resp, ok := s.tryDeterministicArtistAlbumCount(ctx, msg); ok {
		return ChatResponse{Response: resp}, true
	}
	if resp, ok := s.tryDeterministicBadlyRatedAlbumsCleanupFollowUp(ctx, msg); ok {
		return ChatResponse{Response: resp}, true
	}
	if resp, ok := s.tryDeterministicBroadDiscoveryClarification(q); ok {
		return ChatResponse{Response: resp}, true
	}
	if resp, ok := s.tryDeterministicArtistDiscoveryScopeClarification(msg); ok {
		return ChatResponse{Response: resp}, true
	}
	if resp, ok := s.tryDeterministicSpecificAlbumDiscovery(ctx, msg); ok {
		return ChatResponse{Response: resp}, true
	}
	if resp, ok := s.tryDeterministicAlbumRelationshipQuery(ctx, q); ok {
		return ChatResponse{Response: resp}, true
	}
	if resp, ok := s.tryDeterministicUnderplayedAlbums(ctx, msg); ok {
		return ChatResponse{Response: resp}, true
	}
	if resp, ok := s.tryDeterministicCreativeLibraryAlbums(ctx, q); ok {
		return ChatResponse{Response: resp}, true
	}
	if resp, ok := s.tryDeterministicCreativeAlbumSetFollowUp(ctx, msg); ok {
		return ChatResponse{Response: resp}, true
	}
	if resp, ok := s.tryDeterministicLibraryFacetQuery(ctx, q); ok {
		return ChatResponse{Response: resp}, true
	}
	if resp, ok := s.tryDeterministicAlbumLibraryStatsQuery(ctx, q); ok {
		return ChatResponse{Response: resp}, true
	}
	if resp, ok := tryDeterministicUnsupportedAlbumRelationshipQuery(q); ok {
		return ChatResponse{Response: resp}, true
	}
	if resp, ok := s.tryDeterministicArtistListeningStatsQuery(ctx, q); ok {
		return ChatResponse{Response: resp}, true
	}
	if resp, ok := s.tryDeterministicArtistLibraryStatsQuery(ctx, q); ok {
		return ChatResponse{Response: resp}, true
	}
	if resp, ok := s.tryDeterministicPlaylistAvailability(ctx, q); ok {
		return ChatResponse{Response: resp}, true
	}
	if resp, ok := s.tryDeterministicPlaylistQueue(ctx, q, msg); ok {
		return ChatResponse{Response: resp}, true
	}
	if resp, ok := s.tryDeterministicPlaylistCreate(ctx, msg); ok {
		return resp, true
	}
	if resp, pendingAction, ok := s.tryDeterministicArtistRemoval(ctx, msg); ok {
		return ChatResponse{Response: resp, PendingAction: pendingAction}, true
	}
	if resp, ok := s.tryDeterministicDiscoveredAlbumsAvailability(ctx, msg); ok {
		return ChatResponse{Response: resp}, true
	}
	if resp, pendingAction, ok := s.tryDeterministicDiscoveredAlbumsApply(ctx, msg); ok {
		return ChatResponse{Response: resp, PendingAction: pendingAction}, true
	}
	if resp, ok := s.tryDeterministicRecentListeningSummary(ctx, q); ok {
		return resp, true
	}
	if resp, ok := s.tryDeterministicRecentListeningInterpretation(ctx, msg); ok {
		return ChatResponse{Response: resp}, true
	}
	return ChatResponse{}, false
}
