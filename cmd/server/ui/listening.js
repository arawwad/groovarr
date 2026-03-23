import { renderListenClusters, renderListenClustersError, renderListenMap, renderListenMapError, renderListenOverview, renderListenOverviewError, renderListenPreview, renderListenPreviewError, renderListenTextSearch, renderListenTextSearchError, renderSongPath, renderSongPathError, renderTrackSearchResults } from './dom.js';

export async function loadListenOverview(ui) {
    try {
        const overviewResult = await fetch('/api/listen/overview');
        if (!overviewResult.ok) {
            throw new Error('Failed to load Navidrome controls');
        }
        const payload = await overviewResult.json();
        renderListenOverview(ui, payload);
    } catch (error) {
        renderListenOverviewError(ui, error && error.message ? error.message : 'Failed to load Navidrome controls.');
    }
}

export async function loadExploreOverview(ui) {
    const percent = ui.listenMapPercentSelect ? Number(ui.listenMapPercentSelect.value || '20') : 20;
    let clustersPayload = null;
    let mapPayload = null;
    const [clustersResult, mapResult] = await Promise.allSettled([
        fetch('/api/listen/clusters'),
        fetch(`/api/listen/map?percent=${encodeURIComponent(String(percent))}`)
    ]);

    if (clustersResult.status === 'fulfilled') {
        try {
            if (!clustersResult.value.ok) {
                throw new Error('Failed to load exploration state');
            }
            clustersPayload = await clustersResult.value.json();
            renderListenClusters(ui, clustersPayload);
        } catch (error) {
            renderListenClustersError(ui, error && error.message ? error.message : 'Failed to load exploration state.');
        }
    } else {
        renderListenClustersError(ui, clustersResult.reason && clustersResult.reason.message ? clustersResult.reason.message : 'Failed to load exploration state.');
    }

    if (mapResult.status === 'fulfilled') {
        try {
            if (!mapResult.value.ok) {
                throw new Error('Failed to load map state');
            }
            mapPayload = await mapResult.value.json();
            renderListenMap(ui, mapPayload);
        } catch (error) {
            renderListenMapError(ui, error && error.message ? error.message : 'Failed to load map state.');
        }
    } else {
        renderListenMapError(ui, mapResult.reason && mapResult.reason.message ? mapResult.reason.message : 'Failed to load map state.');
    }

    updateExploreSummary(ui, clustersPayload, mapPayload);
}

export function bindListenControls(ui) {
    let selectedPreviewSeed = null;
    let selectedSongPathStart = null;
    let selectedSongPathEnd = null;

    if (ui.listenContextForm) {
        ui.listenContextForm.addEventListener('submit', async (event) => {
            event.preventDefault();
            ui.saveListenContextButton.disabled = true;
            try {
                const response = await fetch('/api/similarity/context', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({
                        mode: ui.listenModeSelect.value,
                        mood: ui.listenMoodInput.value.trim(),
                        ttlMinutes: Number(ui.listenTTLSelect.value || '0'),
                        source: 'listen-ui'
                    })
                });
                if (!response.ok) {
                    const message = await response.text();
                    throw new Error(message || 'Failed to update listening context');
                }
                await loadListenOverview(ui);
                if (selectedPreviewSeed) {
                    await loadListenPreview(ui, selectedPreviewSeed);
                }
                if (typeof ui.showToast === 'function') {
                    ui.showToast('Listening context applied to Navidrome.', 'success');
                }
            } catch (error) {
                ui.listenContextStatus.textContent = error && error.message ? error.message : 'Failed to update listening context.';
                if (typeof ui.showToast === 'function') {
                    ui.showToast(ui.listenContextStatus.textContent, 'error', 4200);
                }
            } finally {
                ui.saveListenContextButton.disabled = false;
            }
        });

        ui.clearListenContextButton.addEventListener('click', async () => {
            ui.clearListenContextButton.disabled = true;
            try {
                const response = await fetch('/api/similarity/context', { method: 'DELETE' });
                if (!response.ok) {
                    const message = await response.text();
                    throw new Error(message || 'Failed to clear listening context');
                }
                await loadListenOverview(ui);
                if (selectedPreviewSeed) {
                    await loadListenPreview(ui, selectedPreviewSeed);
                }
                if (typeof ui.showToast === 'function') {
                    ui.showToast('Listening context cleared.', 'info');
                }
            } catch (error) {
                ui.listenContextStatus.textContent = error && error.message ? error.message : 'Failed to clear listening context.';
                if (typeof ui.showToast === 'function') {
                    ui.showToast(ui.listenContextStatus.textContent, 'error', 4200);
                }
            } finally {
                ui.clearListenContextButton.disabled = false;
            }
        });
    }

    const runPreviewSearch = async () => {
        const query = ui.listenPreviewSearchInput.value.trim();
        if (!query || !ui.listenPreviewSearchResults) return;
        ui.listenPreviewSearchButton.disabled = true;
        try {
            const response = await fetch(`/api/listen/track-search?q=${encodeURIComponent(query)}`);
            if (!response.ok) {
                const message = await response.text();
                throw new Error(message || 'Failed to search tracks');
            }
            const payload = await response.json();
            const results = Array.isArray(payload && payload.tracks) ? payload.tracks : [];
            const searchTarget = document.createElement('div');
            renderTrackSearchResults(searchTarget, results, async (track) => {
                selectedPreviewSeed = track;
                ui.listenPreviewSearchInput.value = `${track.title} - ${track.artistName}`;
                await loadListenPreview(ui, track);
                if (typeof ui.closeSheet === 'function') {
                    ui.closeSheet();
                }
            });
            ui.listenPreviewSearchResults.innerHTML = '';
            if (results.length === 0) {
                ui.listenPreviewStatus.textContent = 'No matching preview seeds found.';
                ui.listenPreviewSearchResults.innerHTML = '<p class="empty-state">No matching preview seeds found.</p>';
                return;
            }
            ui.listenPreviewStatus.textContent = `${results.length} preview seed${results.length === 1 ? '' : 's'} ready in the drawer.`;
            ui.listenPreviewSearchResults.innerHTML = '<p class="empty-state">Preview seed results are open in the drawer.</p>';
            if (typeof ui.openSheet === 'function') {
                ui.openSheet('Select Preview Seed', searchTarget);
            }
        } catch (error) {
            renderListenPreviewError(ui, error && error.message ? error.message : 'Failed to search tracks.');
        } finally {
            ui.listenPreviewSearchButton.disabled = false;
        }
    };

    const runTextSearch = async () => {
        const query = ui.exploreTextQueryInput.value.trim();
        if (!query || !ui.exploreTextResults) return;
        ui.exploreTextSearchButton.disabled = true;
        try {
            const response = await fetch('/api/listen/text-search', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    queryText: query,
                    limit: 8
                })
            });
            if (!response.ok) {
                const message = await response.text();
                throw new Error(message || 'Failed to run vibe search');
            }
            const payload = await response.json();
            renderListenTextSearch(ui, payload);
        } catch (error) {
            renderListenTextSearchError(ui, error && error.message ? error.message : 'Failed to run vibe search.');
        } finally {
            ui.exploreTextSearchButton.disabled = false;
        }
    };

    if (ui.listenPreviewSearchButton && ui.listenPreviewSearchInput) {
        ui.listenPreviewSearchButton.addEventListener('click', runPreviewSearch);
        ui.listenPreviewSearchInput.addEventListener('keydown', (event) => {
            if (event.key === 'Enter') {
                event.preventDefault();
                runPreviewSearch();
            }
        });
    }

    if (ui.exploreTextSearchButton && ui.exploreTextQueryInput) {
        ui.exploreTextSearchButton.addEventListener('click', runTextSearch);
        ui.exploreTextQueryInput.addEventListener('keydown', (event) => {
            if (event.key === 'Enter') {
                event.preventDefault();
                runTextSearch();
            }
        });
    }

    if (Array.isArray(ui.exploreShortcutButtons)) {
        ui.exploreShortcutButtons.forEach((button) => {
            button.addEventListener('click', () => {
                if (typeof ui.handleChatShortcut === 'function') {
                    ui.handleChatShortcut(button.dataset.chatShortcut || '');
                }
            });
        });
    }

    const runSongPathSearch = async (which) => {
        const isStart = which === 'start';
        const input = isStart ? ui.songPathStartInput : ui.songPathEndInput;
        const button = isStart ? ui.songPathStartButton : ui.songPathEndButton;
        const resultsTarget = isStart ? ui.songPathStartResults : ui.songPathEndResults;
        if (!input || !button || !resultsTarget) return;
        const query = input.value.trim();
        if (!query) return;
        button.disabled = true;
        try {
            const response = await fetch(`/api/listen/path-search?q=${encodeURIComponent(query)}`);
            if (!response.ok) {
                const message = await response.text();
                throw new Error(message || 'Failed to search Song Path tracks');
            }
            const payload = await response.json();
            const results = payload && payload.songPathSearch && Array.isArray(payload.songPathSearch.tracks)
                ? payload.songPathSearch.tracks
                : [];
            const message = payload && payload.songPathSearch && payload.songPathSearch.message
                ? payload.songPathSearch.message
                : '';
            const searchTarget = document.createElement('div');
            renderTrackSearchResults(searchTarget, results, async (track) => {
                if (isStart) {
                    selectedSongPathStart = track;
                    ui.songPathStartInput.value = `${track.title} - ${track.artistName}`;
                    ui.songPathStartSelection.textContent = `Start: ${track.title} • ${track.artistName}${track.albumName ? ` • ${track.albumName}` : ''}`;
                } else {
                    selectedSongPathEnd = track;
                    ui.songPathEndInput.value = `${track.title} - ${track.artistName}`;
                    ui.songPathEndSelection.textContent = `End: ${track.title} • ${track.artistName}${track.albumName ? ` • ${track.albumName}` : ''}`;
                }
                if (typeof ui.closeSheet === 'function') {
                    ui.closeSheet();
                }
            });
            resultsTarget.innerHTML = '';
            if (typeof ui.openSheet === 'function') {
                ui.openSheet(isStart ? 'Select Start Song' : 'Select End Song', searchTarget);
            }
            if (message) {
                ui.songPathStatus.textContent = message;
            }
        } catch (error) {
            renderSongPathError(ui, error && error.message ? error.message : 'Failed to search Song Path tracks.');
        } finally {
            button.disabled = false;
        }
    };

    ui.handleSongPathShortcut = async (which, query) => {
        const isStart = which === 'start';
        const input = isStart ? ui.songPathStartInput : ui.songPathEndInput;
        if (!input) return;
        input.value = query;
        ui.songPathStatus.textContent = isStart
            ? `Loading Song Path start candidates from: ${query}`
            : `Loading Song Path end candidates from: ${query}`;
        await runSongPathSearch(which);
    };

    if (ui.songPathStartButton && ui.songPathStartInput) {
        ui.songPathStartButton.addEventListener('click', () => runSongPathSearch('start'));
        ui.songPathStartInput.addEventListener('keydown', (event) => {
            if (event.key === 'Enter') {
                event.preventDefault();
                runSongPathSearch('start');
            }
        });
    }

    if (ui.songPathEndButton && ui.songPathEndInput) {
        ui.songPathEndButton.addEventListener('click', () => runSongPathSearch('end'));
        ui.songPathEndInput.addEventListener('keydown', (event) => {
            if (event.key === 'Enter') {
                event.preventDefault();
                runSongPathSearch('end');
            }
        });
    }

    if (ui.songPathForm) {
        ui.songPathForm.addEventListener('submit', async (event) => {
            event.preventDefault();
            if (!selectedSongPathStart || !selectedSongPathEnd) {
                renderSongPathError(ui, 'Select both a start song and an end song first.');
                return;
            }
            ui.songPathSubmitButton.disabled = true;
            try {
                const response = await fetch('/api/listen/path', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({
                        startTrackId: selectedSongPathStart.id,
                        endTrackId: selectedSongPathEnd.id,
                        maxSteps: Number(ui.songPathStepsInput.value || '25'),
                        keepExactSize: Boolean(ui.songPathExactCheckbox.checked)
                    })
                });
                if (!response.ok) {
                    const message = await response.text();
                    throw new Error(message || 'Failed to load Song Path');
                }
                const payload = await response.json();
                renderSongPath(
                    ui,
                    payload,
                    `${selectedSongPathStart.title} -> ${selectedSongPathEnd.title}`
                );
            } catch (error) {
                renderSongPathError(ui, error && error.message ? error.message : 'Failed to load Song Path.');
            } finally {
                ui.songPathSubmitButton.disabled = false;
            }
        });
    }

    if (ui.startClustersButton) {
        ui.startClustersButton.addEventListener('click', async () => {
            ui.startClustersButton.disabled = true;
            try {
                const response = await fetch('/api/listen/clusters/start', { method: 'POST' });
                if (!response.ok) {
                    const message = await response.text();
                    throw new Error(message || 'Failed to start clustering');
                }
                const payload = await response.json();
                const message = payload && payload.listenClustersStart && payload.listenClustersStart.message
                    ? payload.listenClustersStart.message
                    : 'Scene clustering started.';
                ui.listenClustersStatus.textContent = message;
                if (typeof ui.showToast === 'function') {
                    ui.showToast(message, 'success');
                }
                await loadExploreOverview(ui);
            } catch (error) {
                renderListenClustersError(ui, error && error.message ? error.message : 'Failed to start clustering.');
                if (typeof ui.showToast === 'function') {
                    ui.showToast(error && error.message ? error.message : 'Failed to start clustering.', 'error', 4200);
                }
            }
        });
    }

    if (ui.listenClustersTaskButton && ui.listenClustersTask) {
        ui.listenClustersTaskButton.addEventListener('click', () => {
            if (typeof ui.openDialog !== 'function') return;
            const content = ui.listenClustersTask.cloneNode(true);
            content.hidden = false;
            ui.openDialog('Scene Task Details', content);
        });
    }

    if (ui.listenMapRefreshButton) {
        ui.listenMapRefreshButton.addEventListener('click', () => {
            loadExploreOverview(ui);
        });
    }

    if (ui.listenMapPercentSelect) {
        ui.listenMapPercentSelect.addEventListener('change', () => {
            loadExploreOverview(ui);
        });
    }
}

async function loadListenPreview(ui, track) {
    const previewResult = await requestListenPreview(track);
    renderListenPreview(ui, previewResult, `${track.title} - ${track.artistName}`);
}

async function requestListenPreview(track) {
    const response = await fetch('/api/listen/preview', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
            seedTrackId: track.id,
            limit: 6
        })
    });
    if (!response.ok) {
        const message = await response.text();
        throw new Error(message || 'Failed to load preview');
    }
    return response.json();
}

function updateExploreSummary(ui, clustersPayload, mapPayload) {
    if (!ui.exploreSummaryStatus) return;

    const clusters = clustersPayload && clustersPayload.listenClusters ? clustersPayload.listenClusters : null;
    const map = mapPayload && mapPayload.listenMap ? mapPayload.listenMap : null;
    const summaryParts = [];

    if ((clusters && clusters.configured !== false) || (map && map.configured !== false)) {
        summaryParts.push('Sonic tools ready');
    }

    if (map && map.ready) {
        summaryParts.push(`map ready (${map.itemCount || 0} points)`);
    } else if (map && map.message) {
        summaryParts.push(map.message);
    }

    if (clusters && clusters.ready) {
        const playlists = Array.isArray(clusters.playlists) ? clusters.playlists.length : 0;
        summaryParts.push(`scenes ready (${playlists})`);
    } else if (clusters && clusters.task && clusters.task.status === 'PROGRESS') {
        const progress = clusters.task.progress == null ? '' : ` ${clusters.task.progress}%`;
        summaryParts.push(`clustering running${progress}`);
    } else if (clusters && clusters.message) {
        summaryParts.push(clusters.message);
    }

    if (summaryParts.length === 0) {
        ui.exploreSummaryStatus.textContent = 'Sonic Studio state loaded.';
        return;
    }
    ui.exploreSummaryStatus.textContent = summaryParts.join(' • ');
}
