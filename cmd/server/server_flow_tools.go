package main

import (
	"context"
	"encoding/json"
	"fmt"
)

func marshalServerFlowToolResult(tool string, payload interface{}) (string, error) {
	out, err := json.Marshal(map[string]interface{}{
		"data": map[string]interface{}{
			tool: payload,
		},
	})
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func (s *Server) executeServerFlowTool(ctx context.Context, tool string, args map[string]interface{}) (string, error) {
	switch tool {
	case "startArtistRemovalPreview":
		response, pendingAction, err := s.startArtistRemovalPreview(ctx, toolStringArg(args, "artistName"))
		if err != nil {
			return "", err
		}
		return marshalServerFlowToolResult("startArtistRemovalPreview", map[string]interface{}{
			"response":      response,
			"pendingAction": pendingAction,
		})
	case "startDiscoveredAlbumsApplyPreview":
		response, pendingAction, err := s.startDiscoveredAlbumsApplyPreview(ctx, toolStringArg(args, "selection"))
		if err != nil {
			return "", err
		}
		return marshalServerFlowToolResult("startDiscoveredAlbumsApplyPreview", map[string]interface{}{
			"response":      response,
			"pendingAction": pendingAction,
		})
	case "startLidarrCleanupApplyPreview":
		response, pendingAction, err := s.startLidarrCleanupApplyPreview(
			ctx,
			toolStringArg(args, "action"),
			toolStringArg(args, "selection"),
		)
		if err != nil {
			return "", err
		}
		return marshalServerFlowToolResult("startLidarrCleanupApplyPreview", map[string]interface{}{
			"response":      response,
			"pendingAction": pendingAction,
		})
	case "startPlaylistCreatePreview":
		preview, err := s.buildPlaylistCreatePreview(
			ctx,
			toolStringArg(args, "playlistName"),
			toolStringArg(args, "prompt"),
			toolIntArg(args, "trackCount", 20),
		)
		if err != nil {
			return "", err
		}
		return marshalServerFlowToolResult("startPlaylistCreatePreview", preview)
	case "startPlaylistAppendPreview":
		preview, err := s.buildPlaylistAppendPreview(
			ctx,
			toolStringArg(args, "playlistName"),
			toolStringArg(args, "prompt"),
			toolIntArg(args, "trackCount", 5),
		)
		if err != nil {
			return "", err
		}
		return marshalServerFlowToolResult("startPlaylistAppendPreview", preview)
	case "startPlaylistRefreshPreview":
		preview, err := s.buildPlaylistRefreshPreview(
			ctx,
			toolStringArg(args, "playlistName"),
			toolIntArg(args, "replaceCount", 5),
		)
		if err != nil {
			return "", err
		}
		return marshalServerFlowToolResult("startPlaylistRefreshPreview", preview)
	case "startPlaylistRepairPreview":
		preview, err := s.buildPlaylistRepairPreview(
			ctx,
			toolStringArg(args, "playlistName"),
		)
		if err != nil {
			return "", err
		}
		return marshalServerFlowToolResult("startPlaylistRepairPreview", preview)
	default:
		return "", fmt.Errorf("unsupported server flow tool: %s", tool)
	}
}
