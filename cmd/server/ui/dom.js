export function getUI() {
    return {
        shell: document.querySelector('.shell'),
        chatView: document.getElementById('chatView'),
        listenView: document.getElementById('listenView'),
        exploreView: document.getElementById('exploreView'),
        workspace: document.querySelector('.workspace'),
        chatPanel: document.querySelector('.chat-panel'),
        chat: document.getElementById('chat'),
        eventsLog: document.getElementById('eventsLog'),
        input: document.getElementById('input'),
        sendButton: document.getElementById('sendButton'),
        stopButton: document.getElementById('stopButton'),
        clearButton: document.getElementById('clearButton'),
        clearEventsButton: document.getElementById('clearEventsButton'),
        activityToggleButton: document.getElementById('activityToggleButton'),
        activityBadge: document.getElementById('activityBadge'),
        optionsToggleButton: document.getElementById('optionsToggleButton'),
        mobileOptionsPanel: document.getElementById('mobileOptionsPanel'),
        mobileStatus: document.getElementById('mobileStatus'),
        closeActivityButton: document.getElementById('closeActivityButton'),
        activityBackdrop: document.getElementById('activityBackdrop'),
        eventsPanel: document.getElementById('eventsPanel'),
        muteEvents: document.getElementById('muteEvents'),
        modelSelect: document.getElementById('modelSelect'),
        agentStatus: document.getElementById('agentStatus'),
        eventsStatus: document.getElementById('eventsStatus'),
        routeLinks: Array.from(document.querySelectorAll('[data-route-link]')),
        exploreModeButtons: Array.from(document.querySelectorAll('[data-explore-mode]')),
        explorePanels: Array.from(document.querySelectorAll('[data-explore-panel]')),
        refreshListenButton: document.getElementById('refreshListenButton'),
        listenDiagnosticsButton: document.getElementById('listenDiagnosticsButton'),
        listenSummaryStatus: document.getElementById('listenSummaryStatus'),
        listenSystemSnapshot: document.getElementById('listenSystemSnapshot'),
        listenContextForm: document.getElementById('listenContextForm'),
        listenModeSelect: document.getElementById('listenModeSelect'),
        listenMoodInput: document.getElementById('listenMoodInput'),
        listenTTLSelect: document.getElementById('listenTTLSelect'),
        saveListenContextButton: document.getElementById('saveListenContextButton'),
        clearListenContextButton: document.getElementById('clearListenContextButton'),
        listenContextStatus: document.getElementById('listenContextStatus'),
        listenPreviewSearchInput: document.getElementById('listenPreviewSearchInput'),
        listenPreviewSearchButton: document.getElementById('listenPreviewSearchButton'),
        listenPreviewSearchResults: document.getElementById('listenPreviewSearchResults'),
        listenPreviewStatus: document.getElementById('listenPreviewStatus'),
        listenPreviewSummary: document.getElementById('listenPreviewSummary'),
        listenPreviewDetails: document.getElementById('listenPreviewDetails'),
        listenPreviewDetailsButton: document.getElementById('listenPreviewDetailsButton'),
        listenPreviewCurrent: document.getElementById('listenPreviewCurrent'),
        listenPreviewDefault: document.getElementById('listenPreviewDefault'),
        exploreSummaryStatus: document.getElementById('exploreSummaryStatus'),
        refreshExploreButton: document.getElementById('refreshExploreButton'),
        exploreShortcutButtons: Array.from(document.querySelectorAll('[data-chat-shortcut]')),
        exploreTextQueryInput: document.getElementById('exploreTextQueryInput'),
        exploreTextSearchButton: document.getElementById('exploreTextSearchButton'),
        exploreTextStatus: document.getElementById('exploreTextStatus'),
        exploreTextResults: document.getElementById('exploreTextResults'),
        songPathStartInput: document.getElementById('songPathStartInput'),
        songPathStartButton: document.getElementById('songPathStartButton'),
        songPathStartResults: document.getElementById('songPathStartResults'),
        songPathStartSelection: document.getElementById('songPathStartSelection'),
        songPathEndInput: document.getElementById('songPathEndInput'),
        songPathEndButton: document.getElementById('songPathEndButton'),
        songPathEndResults: document.getElementById('songPathEndResults'),
        songPathEndSelection: document.getElementById('songPathEndSelection'),
        songPathForm: document.getElementById('songPathForm'),
        songPathStepsInput: document.getElementById('songPathStepsInput'),
        songPathExactCheckbox: document.getElementById('songPathExactCheckbox'),
        songPathSubmitButton: document.getElementById('songPathSubmitButton'),
        songPathStatus: document.getElementById('songPathStatus'),
        songPathResults: document.getElementById('songPathResults'),
        listenMapPercentSelect: document.getElementById('listenMapPercentSelect'),
        listenMapRefreshButton: document.getElementById('listenMapRefreshButton'),
        listenMapStatus: document.getElementById('listenMapStatus'),
        listenMapSummary: document.getElementById('listenMapSummary'),
        listenMapPanel: document.getElementById('listenMapPanel'),
        listenClustersStatus: document.getElementById('listenClustersStatus'),
        startClustersButton: document.getElementById('startClustersButton'),
        listenClustersTaskButton: document.getElementById('listenClustersTaskButton'),
        listenClustersTask: document.getElementById('listenClustersTask'),
        listenClustersPanel: document.getElementById('listenClustersPanel'),
        listenSimilarityCard: document.getElementById('listenSimilarityCard'),
        listenSyncCard: document.getElementById('listenSyncCard'),
        listenTasteCard: document.getElementById('listenTasteCard'),
        listenSimilarityPanel: document.getElementById('listenSimilarityPanel'),
        listenSyncPanel: document.getElementById('listenSyncPanel'),
        listenTastePanel: document.getElementById('listenTastePanel'),
        appToastRegion: document.getElementById('appToastRegion'),
        appSheetBackdrop: document.getElementById('appSheetBackdrop'),
        appSheet: document.getElementById('appSheet'),
        appSheetTitle: document.getElementById('appSheetTitle'),
        appSheetBody: document.getElementById('appSheetBody'),
        appSheetClose: document.getElementById('appSheetClose'),
        appDialogBackdrop: document.getElementById('appDialogBackdrop'),
        appDialog: document.getElementById('appDialog'),
        appDialogTitle: document.getElementById('appDialogTitle'),
        appDialogBody: document.getElementById('appDialogBody'),
        appDialogClose: document.getElementById('appDialogClose')
    };
}

export function formatClock(d) {
    const date = d instanceof Date ? d : new Date();
    return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
}

export function setStatus(node, text, stateClass) {
    node.textContent = text;
    node.prepend(Object.assign(document.createElement('span'), { className: 'dot' }));
    node.classList.remove('offline', 'busy');
    if (stateClass) node.classList.add(stateClass);
    window.dispatchEvent(new CustomEvent('groovarr:status-change'));
}

export function trimRenderedMessages(chat, maxRenderedMessages) {
    while (chat.children.length > maxRenderedMessages) {
        const firstChild = chat.firstElementChild;
        if (!firstChild) break;
        const style = window.getComputedStyle(firstChild);
        const removedHeight = firstChild.getBoundingClientRect().height
            + (parseFloat(style.marginTop) || 0)
            + (parseFloat(style.marginBottom) || 0);
        chat.removeChild(firstChild);
        if (chat.scrollTop > 0) {
            chat.scrollTop = Math.max(0, chat.scrollTop - removedHeight);
        }
    }
}

export function isNearScrollBottom(node, threshold = 48) {
    if (!node) return false;
    return node.scrollHeight - node.clientHeight - node.scrollTop <= threshold;
}

export function scheduleScrollToBottom(node) {
    if (!node) return;
    const scheduled = Number(node.dataset.scrollRafId || 0);
    if (scheduled) return;
    const rafId = window.requestAnimationFrame(() => {
        delete node.dataset.scrollRafId;
        node.scrollTop = node.scrollHeight;
    });
    node.dataset.scrollRafId = String(rafId);
}

export function clearChatEmptyState(chat) {
    const emptyState = typeof chat.querySelector === 'function' ? chat.querySelector('.chat-empty') : null;
    if (emptyState) emptyState.remove();
    if (chat && chat.dataset) {
        chat.dataset.empty = 'false';
    }
}

export function resizeComposer(textarea) {
    if (!textarea) return;
    textarea.style.height = 'auto';
    textarea.style.height = Math.min(textarea.scrollHeight, 180) + 'px';
    textarea.style.overflowY = textarea.scrollHeight > 180 ? 'auto' : 'hidden';
}

export function setRequestInFlight(ui, inFlight) {
    ui.sendButton.disabled = inFlight;
    ui.stopButton.disabled = !inFlight;
    ui.stopButton.hidden = !inFlight;
    ui.clearButton.disabled = inFlight;
    ui.clearEventsButton.disabled = inFlight;
    ui.modelSelect.disabled = inFlight;
    ui.muteEvents.disabled = inFlight;
}

export function setConversationIdle(ui, idle) {
    const mobile = window.matchMedia('(max-width: 859px)').matches;
    ui.workspace.classList.toggle('idle-mobile', idle);
    ui.chatPanel.classList.toggle('idle-mobile', idle);
    ui.chat.classList.toggle('idle-mobile', idle);
    ui.chat.dataset.empty = idle ? 'true' : 'false';
    ui.clearButton.hidden = mobile && idle;
}

export function refreshMobileStatus(ui) {
    const agent = ui.agentStatus.textContent.trim();
    const events = ui.eventsStatus.textContent.trim();
    ui.mobileStatus.textContent = agent + ' • ' + events;
}

export function setActivityOpen(ui, open) {
    const mobile = window.matchMedia('(max-width: 859px)').matches;
    ui.activityToggleButton.setAttribute('aria-expanded', open ? 'true' : 'false');
    ui.eventsPanel.setAttribute('aria-hidden', !mobile || open ? 'false' : 'true');
    ui.eventsPanel.classList.toggle('mobile-open', open);
    ui.activityBackdrop.hidden = !mobile || !open;
    ui.activityBackdrop.classList.toggle('visible', mobile && open);
    document.body.classList.toggle('activity-open', mobile && open);
}

export function setOptionsOpen(ui, open) {
    const mobile = window.matchMedia('(max-width: 859px)').matches;
    ui.optionsToggleButton.setAttribute('aria-expanded', open ? 'true' : 'false');
    ui.mobileOptionsPanel.classList.toggle('mobile-open', mobile && open);
    ui.mobileOptionsPanel.setAttribute('aria-hidden', mobile && !open ? 'true' : 'false');
}

export function setCurrentView(ui, view) {
    const isListen = view === 'listen';
    const isExplore = view === 'explore';
    if (ui.chatView) {
        ui.chatView.hidden = isListen || isExplore;
        ui.chatView.classList.toggle('active-view', !isListen && !isExplore);
    }
    if (ui.listenView) {
        ui.listenView.hidden = !isListen;
        ui.listenView.classList.toggle('active-view', isListen);
    }
    if (ui.exploreView) {
        ui.exploreView.hidden = !isExplore;
        ui.exploreView.classList.toggle('active-view', isExplore);
    }
    document.body.classList.toggle('listen-route', isListen);
    document.body.classList.toggle('explore-route', isExplore);
    ui.routeLinks.forEach((link) => {
        const active = link.dataset.route === view;
        link.classList.toggle('active', active);
        link.setAttribute('aria-current', active ? 'page' : 'false');
    });
}

export function setExploreMode(ui, mode) {
    const nextMode = String(mode || 'vibe').trim() || 'vibe';
    if (Array.isArray(ui.exploreModeButtons)) {
        ui.exploreModeButtons.forEach((button) => {
            const active = button.dataset.exploreMode === nextMode;
            button.classList.toggle('active', active);
            button.setAttribute('aria-selected', active ? 'true' : 'false');
        });
    }
    if (Array.isArray(ui.explorePanels)) {
        ui.explorePanels.forEach((panel) => {
            const active = panel.dataset.explorePanel === nextMode;
            panel.hidden = !active;
            panel.classList.toggle('active-explore-panel', active);
        });
    }
}

function mountOverlayContent(target, content) {
    if (!target) return;
    target.innerHTML = '';
    if (content instanceof Node) {
        target.appendChild(content);
        return;
    }
    if (Array.isArray(content)) {
        content.forEach((item) => mountOverlayContent(target, item));
        return;
    }
    const block = document.createElement('div');
    block.textContent = String(content || '');
    target.appendChild(block);
}

function syncOverlayState(ui) {
    const sheetOpen = Boolean(ui.appSheet && !ui.appSheet.hidden);
    const dialogOpen = Boolean(ui.appDialog && !ui.appDialog.hidden);
    document.body.classList.toggle('overlay-open', sheetOpen || dialogOpen);
}

export function showToast(ui, message, variant = 'info', timeoutMs = 3200) {
    if (!ui.appToastRegion || !message) return;
    const toast = document.createElement('div');
    toast.className = `toast toast-${variant}`;
    toast.textContent = String(message);
    ui.appToastRegion.appendChild(toast);
    window.setTimeout(() => {
        toast.classList.add('toast-exit');
        window.setTimeout(() => toast.remove(), 220);
    }, Math.max(1200, timeoutMs));
}

export function closeSheet(ui) {
    if (!ui.appSheet || !ui.appSheetBackdrop) return;
    ui.appSheet.hidden = true;
    ui.appSheet.setAttribute('aria-hidden', 'true');
    ui.appSheetBackdrop.hidden = true;
    syncOverlayState(ui);
}

export function openSheet(ui, title, content) {
    if (!ui.appSheet || !ui.appSheetTitle || !ui.appSheetBody || !ui.appSheetBackdrop) return;
    closeDialog(ui);
    ui.appSheetTitle.textContent = String(title || 'Details');
    mountOverlayContent(ui.appSheetBody, content);
    ui.appSheet.hidden = false;
    ui.appSheet.setAttribute('aria-hidden', 'false');
    ui.appSheetBackdrop.hidden = false;
    syncOverlayState(ui);
}

export function closeDialog(ui) {
    if (!ui.appDialog || !ui.appDialogBackdrop) return;
    ui.appDialog.hidden = true;
    ui.appDialog.setAttribute('aria-hidden', 'true');
    ui.appDialogBackdrop.hidden = true;
    syncOverlayState(ui);
}

export function openDialog(ui, title, content) {
    if (!ui.appDialog || !ui.appDialogTitle || !ui.appDialogBody || !ui.appDialogBackdrop) return;
    closeSheet(ui);
    ui.appDialogTitle.textContent = String(title || 'Details');
    mountOverlayContent(ui.appDialogBody, content);
    const footer = document.createElement('div');
    footer.className = 'overlay-dismiss-row';
    const dismissButton = document.createElement('button');
    dismissButton.type = 'button';
    dismissButton.className = 'ghost-btn';
    dismissButton.dataset.dialogClose = 'true';
    dismissButton.textContent = 'Done';
    footer.appendChild(dismissButton);
    ui.appDialogBody.appendChild(footer);
    ui.appDialog.hidden = false;
    ui.appDialog.setAttribute('aria-hidden', 'false');
    ui.appDialogBackdrop.hidden = false;
    syncOverlayState(ui);
}

export function setActivityBadge(ui, count) {
    if (count > 0) {
        ui.activityBadge.hidden = false;
        ui.activityBadge.textContent = String(count);
    } else {
        ui.activityBadge.hidden = true;
        ui.activityBadge.textContent = '0';
    }
}

function formatPendingActionKind(kind) {
    switch (String(kind || '').trim()) {
        case 'playlist_create':
            return 'Create Preview';
        case 'playlist_append':
            return 'Append Preview';
        case 'playlist_refresh':
            return 'Refresh Preview';
        case 'playlist_repair':
            return 'Repair Preview';
        case 'artist_remove':
            return 'Removal Preview';
        default:
            return 'Pending Action';
    }
}

function relativeExpiryLabel(expiresAt) {
    const raw = String(expiresAt || '').trim();
    if (!raw) return '';
    const date = new Date(raw);
    if (Number.isNaN(date.getTime())) return '';
    const diffMs = date.getTime() - Date.now();
    if (diffMs <= 0) return 'Expired';
    const minutes = Math.round(diffMs / 60000);
    if (minutes <= 1) return 'Expires in about 1 min';
    if (minutes < 60) return `Expires in about ${minutes} min`;
    const hours = Math.round(minutes / 60);
    if (hours <= 1) return 'Expires in about 1 hr';
    return `Expires in about ${hours} hr`;
}

function parsePendingActionDetails(details) {
    const metrics = [];
    const notes = [];
    if (!Array.isArray(details)) {
        return { metrics, notes };
    }
    details.forEach((detail) => {
        const text = String(detail || '').trim();
        if (!text) return;
        const colonIndex = text.indexOf(':');
        if (colonIndex > 0 && colonIndex < text.length - 1) {
            metrics.push({
                label: text.slice(0, colonIndex).trim(),
                value: text.slice(colonIndex + 1).trim()
            });
            return;
        }
        notes.push(text);
    });
    return { metrics, notes };
}

export function setPendingActionResolutionState(card, command) {
    if (!card) return;
    const actions = card.querySelector('.actions');
    if (actions) actions.remove();

    card.classList.add('resolved');
    if (command === 'discard') {
        card.classList.add('discarded');
    }

    const existing = card.querySelector('.status');
    if (existing) existing.remove();

    const status = document.createElement('div');
    status.className = 'status';
    status.textContent = command === 'discard' ? 'Preview discarded' : 'Preview approved';
    card.appendChild(status);
}

export function buildPendingActionCard(action, onResolve) {
    if (!action || !action.id) return null;

    const card = document.createElement('div');
    card.className = 'pending-action';
    card.dataset.actionId = action.id;
    if (action.kind) {
        card.dataset.kind = action.kind;
    }

    const header = document.createElement('div');
    header.className = 'pending-action-header';

    const eyebrow = document.createElement('div');
    eyebrow.className = 'pending-action-eyebrow';

    const kind = document.createElement('span');
    kind.className = 'pending-action-kind';
    kind.textContent = formatPendingActionKind(action.kind);
    eyebrow.appendChild(kind);

    const expiryLabel = relativeExpiryLabel(action.expiresAt);
    if (expiryLabel) {
        const expires = document.createElement('span');
        expires.className = 'pending-action-expiry';
        expires.textContent = expiryLabel;
        eyebrow.appendChild(expires);
    }
    header.appendChild(eyebrow);

    const title = document.createElement('h4');
    title.textContent = action.title || 'Pending action';
    header.appendChild(title);
    card.appendChild(header);

    if (action.summary) {
        const summary = document.createElement('p');
        summary.className = 'pending-action-summary';
        summary.textContent = action.summary;
        card.appendChild(summary);
    }

    const { metrics, notes } = parsePendingActionDetails(action.details);
    if (metrics.length > 0) {
        const grid = document.createElement('div');
        grid.className = 'pending-action-metrics';
        metrics.forEach((metric) => {
            const item = document.createElement('div');
            item.className = 'pending-action-metric';

            const label = document.createElement('span');
            label.className = 'pending-action-metric-label';
            label.textContent = metric.label;
            item.appendChild(label);

            const value = document.createElement('strong');
            value.className = 'pending-action-metric-value';
            value.textContent = metric.value;
            item.appendChild(value);

            grid.appendChild(item);
        });
        card.appendChild(grid);
    }

    if (notes.length > 0) {
        const list = document.createElement('ul');
        notes.forEach((detail) => {
            const item = document.createElement('li');
            item.textContent = detail;
            list.appendChild(item);
        });
        card.appendChild(list);
    }

    const actions = document.createElement('div');
    actions.className = 'actions';

    const approveButton = document.createElement('button');
    approveButton.type = 'button';
    approveButton.className = 'approve';
    approveButton.textContent = action.approveLabel || 'Approve';
    approveButton.addEventListener('click', () => onResolve(action.id, 'approve', card));
    actions.appendChild(approveButton);

    const discardButton = document.createElement('button');
    discardButton.type = 'button';
    discardButton.className = 'discard';
    discardButton.textContent = action.discardLabel || 'Discard';
    discardButton.addEventListener('click', () => onResolve(action.id, 'discard', card));
    actions.appendChild(discardButton);

    card.appendChild(actions);
    return card;
}

export function appendRichText(body, content) {
    body.innerHTML = '';
    const lines = String(content || '').split('\n');
    let listNode = null;

    const flushList = () => {
        if (listNode && listNode.children.length > 0) {
            body.appendChild(listNode);
        }
        listNode = null;
    };

    lines.forEach((line) => {
        const trimmed = line.trim();
        if (trimmed.startsWith('- ')) {
            if (!listNode) {
                listNode = document.createElement('ul');
            }
            const item = document.createElement('li');
            item.textContent = trimmed.slice(2);
            listNode.appendChild(item);
            return;
        }

        flushList();
        if (trimmed === '') {
            return;
        }
        const paragraph = document.createElement('p');
        paragraph.textContent = line;
        body.appendChild(paragraph);
    });

    flushList();
}

export function addMessage({ chat, role, content, extraClass, metaLabel, pendingAction, onResolvePendingAction, onAfterRender }) {
    clearChatEmptyState(chat);

    const div = document.createElement('div');
    div.className = 'message ' + role + (extraClass ? ' ' + extraClass : '');

    const meta = document.createElement('div');
    meta.className = 'meta';
    meta.textContent = (metaLabel ? metaLabel + ' ' : '') + formatClock(new Date());

    const body = document.createElement('div');
    body.className = 'body';
    appendRichText(body, content);

    div.appendChild(meta);
    div.appendChild(body);

    if (role === 'assistant' && pendingAction) {
        const card = buildPendingActionCard(pendingAction, onResolvePendingAction);
        if (card) div.appendChild(card);
    }

    chat.appendChild(div);
    if (typeof onAfterRender === 'function') onAfterRender();
    return div;
}

export function renderChatEmptyState({ chat, prompts, onPromptSelect }) {
    if (!chat || chat.children.length > 0) return;
    if (chat.dataset) {
        chat.dataset.empty = 'true';
    }

    const empty = document.createElement('div');
    empty.className = 'chat-empty';

    const title = document.createElement('h3');
    title.textContent = 'Start with something specific';
    empty.appendChild(title);

    const description = document.createElement('p');
    description.textContent = 'Ask for library help, artist discovery, or a fast recommendation.';
    empty.appendChild(description);

    if (Array.isArray(prompts) && prompts.length > 0) {
        const suggestions = document.createElement('div');
        suggestions.className = 'suggestions';
        prompts.forEach((prompt) => {
            const button = document.createElement('button');
            button.type = 'button';
            button.className = 'suggestion';
            button.textContent = prompt;
            button.addEventListener('click', () => onPromptSelect(prompt));
            suggestions.appendChild(button);
        });
        empty.appendChild(suggestions);
    }

    chat.appendChild(empty);
}

export function resetEventLog(eventsLog) {
    if (!eventsLog) return;
    eventsLog.innerHTML = '';
    const empty = document.createElement('p');
    empty.className = 'empty-state';
    empty.textContent = 'Live events will appear here.';
    eventsLog.appendChild(empty);
}

export function addEventEntry({ eventsLog, content, metaLabel, onAfterRender }) {
    if (!eventsLog) return null;
    const empty = eventsLog.querySelector('.empty-state');
    if (empty) empty.remove();

    const entry = document.createElement('div');
    entry.className = 'event-entry';

    const meta = document.createElement('div');
    meta.className = 'meta';
    meta.textContent = (metaLabel ? metaLabel + ' ' : '') + formatClock(new Date());

    const body = document.createElement('div');
    body.className = 'body';
    body.textContent = String(content || '');

    entry.appendChild(meta);
    entry.appendChild(body);
    eventsLog.appendChild(entry);

    if (typeof onAfterRender === 'function') onAfterRender();
    return entry;
}

export function renderListenOverview(ui, payload) {
    const overview = payload && payload.listenOverview ? payload.listenOverview : {};
    const similarity = overview.similarity || {};
    const syncStatus = overview.syncStatus || {};
    const tasteProfile = overview.tasteProfile || null;
    const currentContext = overview.context || null;
    const navidromeConfigured = Boolean(overview.navidromeConfigured);

    setStatus(
        ui.listenSummaryStatus,
        navidromeConfigured ? 'Navidrome Controls Ready' : 'Navidrome Not Configured',
        navidromeConfigured ? '' : 'offline'
    );
    if (ui.listenModeSelect) {
        ui.listenModeSelect.value = currentContext && currentContext.mode ? currentContext.mode : 'adjacent';
    }
    if (ui.listenMoodInput) {
        ui.listenMoodInput.value = currentContext && currentContext.mood ? currentContext.mood : '';
    }
    if (ui.listenTTLSelect) {
        ui.listenTTLSelect.value = '0';
    }
    if (ui.listenContextStatus) {
        ui.listenContextStatus.textContent = currentContext
            ? buildContextStatus(currentContext)
            : 'No active override. Navidrome is using Groovarr defaults.';
    }
    if (ui.listenSystemSnapshot) {
        ui.listenSystemSnapshot.innerHTML = '';
        ui.listenSystemSnapshot.appendChild(buildMetricGrid([
            metricItem('Provider', similarity.defaultProvider || 'unknown'),
            metricItem('Sonic', similarity.audioMuseReachable ? 'ready' : (similarity.audioMuseConfigured ? 'offline' : 'disabled')),
            metricItem('Last Sync', formatDateTime(readValue(syncStatus, 'lastSync', 'LastSync')) || 'unknown'),
            metricItem('Play Events', formatInteger(readValue(syncStatus, 'playEventsCount', 'PlayEventsCount')))
        ]));
    }

    ui.listenSimilarityPanel.innerHTML = '';
    ui.listenSimilarityPanel.appendChild(buildMetricGrid([
        metricItem('Default Provider', similarity.defaultProvider || 'unknown'),
        metricItem('Preferred Source', similarity.preferredTrackSource || 'unknown'),
        metricItem('Sonic Engine', similarity.audioMuseReachable ? 'reachable' : (similarity.audioMuseConfigured ? 'configured' : 'not configured')),
        metricItem('Library State', similarity.audioMuseLibraryState || 'unknown'),
        metricItem('Bootstrap', similarity.audioMuseBootstrap || 'unknown'),
        metricItem('Providers', Array.isArray(similarity.availableProviders) ? similarity.availableProviders.join(', ') : 'local')
    ]));

    ui.listenSyncPanel.innerHTML = '';
    ui.listenSyncPanel.appendChild(buildMetricGrid([
        metricItem('Navidrome', navidromeConfigured ? 'connected' : 'missing'),
        metricItem('Last Sync', formatDateTime(readValue(syncStatus, 'lastSync', 'LastSync')) || 'unknown'),
        metricItem('Latest Play', formatDateTime(readValue(syncStatus, 'latestPlayEvent', 'LatestPlayEvent')) || 'unknown'),
        metricItem('Play Events', formatInteger(readValue(syncStatus, 'playEventsCount', 'PlayEventsCount'))),
        metricItem('Scrobble Checkpoint', formatInteger(readValue(syncStatus, 'lastScrobbleSubmissionTime', 'LastScrobbleSubmissionTime')))
    ]));

    ui.listenTastePanel.innerHTML = '';
    if (!tasteProfile) {
        ui.listenTastePanel.appendChild(buildEmptyState('Taste profile has not been built yet. Let the sync cycle finish first.'));
        return;
    }
    ui.listenTastePanel.appendChild(buildMetricGrid([
        metricItem('Total Plays', formatInteger(readValue(tasteProfile, 'totalPlays', 'TotalPlays'))),
        metricItem('Played Tracks', formatInteger(readValue(tasteProfile, 'distinctPlayedTracks', 'DistinctPlayedTracks'))),
        metricItem('Played Artists', formatInteger(readValue(tasteProfile, 'distinctPlayedArtists', 'DistinctPlayedArtists'))),
        metricItem('Rated Tracks', formatInteger(readValue(tasteProfile, 'ratedTracks', 'RatedTracks'))),
        metricItem('Replay Affinity', formatScore(readValue(tasteProfile, 'replayAffinityScore', 'ReplayAffinityScore'))),
        metricItem('Novelty Tolerance', formatScore(readValue(tasteProfile, 'noveltyToleranceScore', 'NoveltyToleranceScore'))),
        metricItem('Updated', formatDateTime(readValue(tasteProfile, 'updatedAt', 'UpdatedAt')) || 'unknown')
    ]));
}

export function renderListenOverviewError(ui, message) {
    setStatus(ui.listenSummaryStatus, 'Controls Unavailable', 'offline');
    const content = buildEmptyState(message || 'Failed to load Navidrome controls.');
    if (ui.listenSystemSnapshot) {
        ui.listenSystemSnapshot.innerHTML = '';
        ui.listenSystemSnapshot.appendChild(content.cloneNode(true));
    }
    ui.listenSimilarityPanel.innerHTML = '';
    ui.listenSyncPanel.innerHTML = '';
    ui.listenTastePanel.innerHTML = '';
    ui.listenSimilarityPanel.appendChild(content.cloneNode(true));
    ui.listenSyncPanel.appendChild(content.cloneNode(true));
    ui.listenTastePanel.appendChild(content);
}

export function renderTrackSearchResults(target, tracks, onSelect) {
    target.innerHTML = '';
    if (!Array.isArray(tracks) || tracks.length === 0) {
        target.appendChild(buildEmptyState('No matching tracks found.'));
        return;
    }
    const list = document.createElement('div');
    list.className = 'preview-search-list';
    tracks.forEach((track) => {
        const button = document.createElement('button');
        button.type = 'button';
        button.className = 'preview-search-item';
        button.innerHTML = `<strong>${escapeHTML(track.title || 'Unknown title')}</strong><span>${escapeHTML(track.artistName || 'Unknown artist')}</span>`;
        button.addEventListener('click', () => onSelect(track));
        list.appendChild(button);
    });
    target.appendChild(list);
}

export function renderListenTextSearch(ui, payload) {
    const textSearch = payload && payload.listenTextSearch ? payload.listenTextSearch : {};
    const matches = Array.isArray(textSearch.matches) ? textSearch.matches : [];
    ui.exploreTextStatus.textContent = textSearch.message || 'Vibe search loaded.';
    ui.exploreTextResults.innerHTML = '';
    if (matches.length === 0) {
        ui.exploreTextResults.appendChild(buildEmptyState('No text-to-sound matches yet.'));
        return;
    }

    const buildResultsList = () => {
        const list = document.createElement('div');
        list.className = 'preview-result-list';
        matches.forEach((match, index) => {
            const card = document.createElement('article');
            card.className = 'preview-result-card';

            const title = document.createElement('div');
            title.className = 'preview-result-title';
            title.textContent = `${index + 1}. ${match.title || 'Unknown title'}`;
            card.appendChild(title);

            const artist = document.createElement('div');
            artist.className = 'preview-result-artist';
            artist.textContent = match.artistName || 'Unknown artist';
            card.appendChild(artist);

            const meta = document.createElement('div');
            meta.className = 'preview-result-meta';
            meta.textContent = buildTextSearchMeta(match);
            card.appendChild(meta);

            const actions = document.createElement('div');
            actions.className = 'map-detail-actions';
            if (typeof ui.handleChatShortcut === 'function') {
                const prompt = `Build from "${match.title || 'this track'}" by ${match.artistName || 'this artist'} using the vibe "${textSearch.queryText || ''}".`;
                actions.appendChild(buildMapActionButton('Ask Chat', () => ui.handleChatShortcut(prompt)));
            }
            const query = [match.title || '', match.artistName || ''].filter(Boolean).join(' ');
            if (typeof ui.handleSongPathShortcut === 'function' && query) {
                actions.appendChild(buildMapActionButton('Path Start', () => ui.handleSongPathShortcut('start', query)));
                actions.appendChild(buildMapActionButton('Path End', () => ui.handleSongPathShortcut('end', query)));
            }
            if (actions.children.length > 0) {
                card.appendChild(actions);
            }

            list.appendChild(card);
        });
        return list;
    };

    const summary = document.createElement('article');
    summary.className = 'preview-result-card';
    summary.innerHTML = `<div class="preview-result-title">${matches.length} grounded match${matches.length === 1 ? '' : 'es'} ready</div><div class="preview-result-meta">Open the results drawer to inspect tracks and route the best seed into chat or Song Path.</div>`;
    const summaryActions = document.createElement('div');
    summaryActions.className = 'map-detail-actions';
    summaryActions.appendChild(buildMapActionButton('View Matches', () => openSheet(ui, 'Vibe Search Results', buildResultsList())));
    summary.appendChild(summaryActions);
    ui.exploreTextResults.appendChild(summary);
    openSheet(ui, 'Vibe Search Results', buildResultsList());
}

export function renderListenTextSearchError(ui, message) {
    ui.exploreTextStatus.textContent = message || 'Failed to load vibe search.';
    ui.exploreTextResults.innerHTML = '';
    ui.exploreTextResults.appendChild(buildEmptyState('Text-to-sound search unavailable.'));
}

export function renderListenPreview(ui, payload, seedLabel) {
    const preview = payload && payload.listenPreview ? payload.listenPreview : {};
    const current = preview.current && Array.isArray(preview.current.results) ? preview.current.results : [];
    const baseline = preview.default && Array.isArray(preview.default.results) ? preview.default.results : [];
    ui.listenPreviewStatus.textContent = seedLabel
        ? `Preview seed: ${seedLabel}`
        : 'Preview loaded.';
    if (ui.listenPreviewSummary) {
        ui.listenPreviewSummary.innerHTML = '';
        ui.listenPreviewSummary.appendChild(buildPreviewSummaryCard(current, baseline));
    }
    if (ui.listenPreviewDetailsButton) {
        ui.listenPreviewDetailsButton.disabled = current.length === 0 && baseline.length === 0;
    }
    renderPreviewColumn(ui.listenPreviewCurrent, current);
    renderPreviewColumn(ui.listenPreviewDefault, baseline);
}

export function renderListenPreviewError(ui, message) {
    ui.listenPreviewStatus.textContent = message || 'Failed to load preview.';
    if (ui.listenPreviewSummary) {
        ui.listenPreviewSummary.innerHTML = '';
        ui.listenPreviewSummary.appendChild(buildEmptyState('Instant Mix preview unavailable.'));
    }
    if (ui.listenPreviewDetailsButton) {
        ui.listenPreviewDetailsButton.disabled = true;
    }
    ui.listenPreviewCurrent.innerHTML = '';
    ui.listenPreviewDefault.innerHTML = '';
    ui.listenPreviewCurrent.appendChild(buildEmptyState('Preview unavailable.'));
    ui.listenPreviewDefault.appendChild(buildEmptyState('Preview unavailable.'));
}

export function renderListenNeighborhood(ui, payload, seedLabel) {
    const neighborhood = payload && payload.listenNeighborhood ? payload.listenNeighborhood : {};
    const seed = neighborhood.seed || {};
    const artists = neighborhood.artists && Array.isArray(neighborhood.artists.results) ? neighborhood.artists.results : [];
    const albums = neighborhood.albums && Array.isArray(neighborhood.albums.results) ? neighborhood.albums.results : [];
    ui.listenNeighborhoodStatus.textContent = seedLabel
        ? `Neighborhood loaded from ${seedLabel}. Follow artist or album branches to keep moving.`
        : 'Neighborhood loaded.';
    renderNeighborhoodSeed(ui, seed, artists, albums);
    renderNeighborhoodGraph(ui, seed, artists, albums);
    renderExploreColumn(
        ui.listenNeighborhoodArtists,
        artists,
        buildArtistNeighborhoodMeta,
        'No nearby artists yet.',
        {
            label: 'Use as seed',
            onSelect: (item) => {
                if (typeof ui.handleExploreShortcut === 'function' && item && item.name) {
                    ui.handleExploreShortcut(String(item.name));
                }
            },
        }
    );
    renderExploreColumn(
        ui.listenNeighborhoodAlbums,
        albums,
        buildAlbumNeighborhoodMeta,
        'No nearby albums yet.',
        {
            label: 'Use as seed',
            onSelect: (item) => {
                if (typeof ui.handleExploreShortcut === 'function') {
                    const parts = [item && item.name ? String(item.name) : '', item && item.artistName ? String(item.artistName) : ''];
                    const query = parts.filter(Boolean).join(' ');
                    if (query) {
                        ui.handleExploreShortcut(query);
                    }
                }
            },
        }
    );
}

export function renderListenNeighborhoodError(ui, message) {
    ui.listenNeighborhoodStatus.textContent = message || 'Failed to load neighborhood.';
    ui.listenNeighborhoodSeed.innerHTML = '';
    ui.listenNeighborhoodGraph.innerHTML = '';
    ui.listenNeighborhoodArtists.innerHTML = '';
    ui.listenNeighborhoodAlbums.innerHTML = '';
    ui.listenNeighborhoodSeed.appendChild(buildEmptyState('Seed spotlight unavailable.'));
    ui.listenNeighborhoodGraph.appendChild(buildEmptyState('Neighborhood map unavailable.'));
    ui.listenNeighborhoodArtists.appendChild(buildEmptyState('Neighborhood unavailable.'));
    ui.listenNeighborhoodAlbums.appendChild(buildEmptyState('Neighborhood unavailable.'));
}

export function renderListenClusters(ui, payload) {
    const clusters = payload && payload.listenClusters ? payload.listenClusters : {};
    ui.listenClustersStatus.textContent = clusters.message || 'Cluster state loaded.';
    if (ui.startClustersButton) {
        const canStart = Boolean(clusters.canStart);
        ui.startClustersButton.disabled = !canStart;
        ui.startClustersButton.textContent = clusterActionLabel(clusters, canStart);
    }
    renderClusterTask(ui.listenClustersTask, clusters.task);
    if (ui.listenClustersTaskButton) {
        ui.listenClustersTaskButton.hidden = !clusters.task;
    }
    renderClusterPlaylists(ui, ui.listenClustersPanel, clusters.playlists);
}

export function renderListenClustersError(ui, message) {
    ui.listenClustersStatus.textContent = message || 'Failed to load clusters.';
    if (ui.startClustersButton) {
        ui.startClustersButton.disabled = false;
        ui.startClustersButton.textContent = 'Start Clustering';
    }
    if (ui.listenClustersTaskButton) {
        ui.listenClustersTaskButton.hidden = true;
    }
    ui.listenClustersTask.innerHTML = '';
    ui.listenClustersPanel.innerHTML = '';
    ui.listenClustersTask.appendChild(buildEmptyState('Cluster task unavailable.'));
    ui.listenClustersPanel.appendChild(buildEmptyState('Cluster playlists unavailable.'));
}

export function renderSongPath(ui, payload, contextLabel) {
    const songPath = payload && payload.listenSongPath ? payload.listenSongPath : {};
    const path = Array.isArray(songPath.path) ? songPath.path : [];
    const message = songPath.message ? String(songPath.message) : '';
    const label = contextLabel ? `${contextLabel} • ` : '';
    ui.songPathStatus.textContent = path.length > 0
        ? `${label}${path.length} songs loaded through Song Path.`
        : (message || `${label}No Song Path results returned.`);
    renderSongPathResults(ui.songPathResults, path);
}

export function renderSongPathError(ui, message) {
    ui.songPathStatus.textContent = message || 'Failed to load Song Path.';
    ui.songPathResults.innerHTML = '';
    ui.songPathResults.appendChild(buildEmptyState('Song Path unavailable.'));
}

export function renderListenMap(ui, payload) {
    const map = payload && payload.listenMap ? payload.listenMap : {};
    const items = Array.isArray(map.items) ? map.items : [];
    const task = map.task || null;
    ui.listenMapStatus.textContent = map.message || 'Map state loaded.';
    ui.listenMapSummary.innerHTML = '';
    ui.listenMapSummary.appendChild(buildMetricGrid([
        metricItem('Projection', map.projection || 'none'),
        metricItem('Sample', `${formatInteger(map.percent || 0)}%`),
        metricItem('Point Budget', formatInteger(map.sampleLimit || items.length || 0)),
        metricItem('Mapped Items', formatInteger(map.itemCount || 0)),
        metricItem('Ready', map.ready ? 'yes' : 'no'),
        metricItem('Task', formatMapTask(task))
    ]));
    renderMapItems(ui, ui.listenMapPanel, items);
}

export function renderListenMapError(ui, message) {
    ui.listenMapStatus.textContent = message || 'Failed to load map.';
    ui.listenMapSummary.innerHTML = '';
    ui.listenMapPanel.innerHTML = '';
    ui.listenMapSummary.appendChild(buildEmptyState('Map summary unavailable.'));
    ui.listenMapPanel.appendChild(buildEmptyState('Map points unavailable.'));
}

function buildMetricGrid(items) {
    const grid = document.createElement('div');
    grid.className = 'metric-grid';
    items.forEach((item) => grid.appendChild(item));
    return grid;
}

function metricItem(label, value) {
    const card = document.createElement('div');
    card.className = 'metric-item';

    const labelNode = document.createElement('span');
    labelNode.className = 'metric-label';
    labelNode.textContent = label;

    const valueNode = document.createElement('strong');
    valueNode.className = 'metric-value';
    valueNode.textContent = value == null || value === '' ? 'unknown' : String(value);

    card.appendChild(labelNode);
    card.appendChild(valueNode);
    return card;
}

function buildEmptyState(message) {
    const empty = document.createElement('p');
    empty.className = 'empty-state';
    empty.textContent = message;
    return empty;
}

function renderPreviewColumn(target, results) {
    target.innerHTML = '';
    if (!Array.isArray(results) || results.length === 0) {
        target.appendChild(buildEmptyState('No preview results.'));
        return;
    }
    const list = document.createElement('div');
    list.className = 'preview-result-list';
    results.forEach((item, index) => {
        const card = document.createElement('article');
        card.className = 'preview-result-card';
        const title = document.createElement('div');
        title.className = 'preview-result-title';
        title.textContent = `${index + 1}. ${item.title || 'Unknown title'}`;
        const artist = document.createElement('div');
        artist.className = 'preview-result-artist';
        artist.textContent = item.artistName || 'Unknown artist';
        const meta = document.createElement('div');
        meta.className = 'preview-result-meta';
        meta.textContent = buildPreviewMeta(item);
        card.appendChild(title);
        card.appendChild(artist);
        card.appendChild(meta);
        list.appendChild(card);
    });
    target.appendChild(list);
}

function buildPreviewSummaryCard(current, baseline) {
    const currentTop = Array.isArray(current) && current.length > 0 ? current[0] : null;
    const baselineTop = Array.isArray(baseline) && baseline.length > 0 ? baseline[0] : null;
    const card = document.createElement('article');
    card.className = 'preview-result-card';
    const title = document.createElement('div');
    title.className = 'preview-result-title';
    title.textContent = 'Instant Mix delta ready';
    const meta = document.createElement('div');
    meta.className = 'preview-result-meta';
    meta.textContent = [
        `${formatInteger(Array.isArray(current) ? current.length : 0)} in the active context`,
        `${formatInteger(Array.isArray(baseline) ? baseline.length : 0)} in the default adjacent mix`
    ].join(' • ');
    card.appendChild(title);
    card.appendChild(meta);
    if (currentTop || baselineTop) {
        const note = document.createElement('div');
        note.className = 'preview-result-artist';
        note.textContent = [
            currentTop ? `Active opens with ${currentTop.title || 'Unknown title'}` : '',
            baselineTop ? `Default opens with ${baselineTop.title || 'Unknown title'}` : ''
        ].filter(Boolean).join(' • ');
        card.appendChild(note);
    }
    return card;
}

function renderExploreColumn(target, results, metaBuilder, emptyMessage, action) {
    target.innerHTML = '';
    if (!Array.isArray(results) || results.length === 0) {
        target.appendChild(buildEmptyState(emptyMessage));
        return;
    }
    const list = document.createElement('div');
    list.className = 'preview-result-list';
    results.forEach((item, index) => {
        const card = document.createElement('article');
        card.className = 'preview-result-card';
        const title = document.createElement('div');
        title.className = 'preview-result-title';
        title.textContent = `${index + 1}. ${item.name || item.title || 'Unknown'}`;
        const artist = document.createElement('div');
        artist.className = 'preview-result-artist';
        artist.textContent = item.artistName || ' ';
        const meta = document.createElement('div');
        meta.className = 'preview-result-meta';
        meta.textContent = metaBuilder(item);
        card.appendChild(title);
        if (item.artistName) {
            card.appendChild(artist);
        }
        card.appendChild(meta);
        if (action && typeof action.onSelect === 'function') {
            const button = document.createElement('button');
            button.type = 'button';
            button.className = 'ghost-btn preview-result-action';
            button.textContent = action.label || 'Use';
            button.addEventListener('click', () => action.onSelect(item));
            card.appendChild(button);
        }
        list.appendChild(card);
    });
    target.appendChild(list);
}

function renderNeighborhoodSeed(ui, seed, artists, albums) {
    ui.listenNeighborhoodSeed.innerHTML = '';
    if (!seed || (!seed.trackId && !seed.title && !seed.artistName)) {
        ui.listenNeighborhoodSeed.appendChild(buildEmptyState('The selected seed will land here with neighborhood counts and shortcuts.'));
        return;
    }

    const eyebrow = document.createElement('div');
    eyebrow.className = 'map-detail-eyebrow';
    eyebrow.textContent = 'Current seed';

    const title = document.createElement('div');
    title.className = 'preview-result-title';
    title.textContent = seed.title || 'Unknown title';

    const artist = document.createElement('div');
    artist.className = 'preview-result-artist';
    artist.textContent = seed.artistName || 'Unknown artist';

    const meta = document.createElement('div');
    meta.className = 'preview-result-meta';
    meta.textContent = buildNeighborhoodSeedMeta(seed);

    const stats = buildMetricGrid([
        metricItem('Artist branches', formatInteger(artists.length)),
        metricItem('Album branches', formatInteger(albums.length)),
        metricItem('Anchor album', seed.albumName || 'none')
    ]);
    stats.classList.add('neighborhood-seed-metrics');

    const hint = document.createElement('p');
    hint.className = 'listen-note';
    hint.textContent = 'Use the graph for shape, then use the branch lists when you want more detail.';

    const actions = document.createElement('div');
    actions.className = 'map-detail-actions';
    const shortcutQuery = [seed.title || '', seed.artistName || ''].filter(Boolean).join(' ');
    actions.appendChild(buildMapActionButton('Search This Seed', () => {
        if (ui.exploreSearchInput && shortcutQuery) {
            ui.exploreSearchInput.value = shortcutQuery;
        }
    }));
    actions.appendChild(buildMapActionButton('Use for Path Start', () => {
        if (typeof ui.handleSongPathShortcut === 'function' && shortcutQuery) {
            ui.handleSongPathShortcut('start', shortcutQuery);
        }
    }));
    actions.appendChild(buildMapActionButton('Use for Path End', () => {
        if (typeof ui.handleSongPathShortcut === 'function' && shortcutQuery) {
            ui.handleSongPathShortcut('end', shortcutQuery);
        }
    }));

    ui.listenNeighborhoodSeed.appendChild(eyebrow);
    ui.listenNeighborhoodSeed.appendChild(title);
    ui.listenNeighborhoodSeed.appendChild(artist);
    ui.listenNeighborhoodSeed.appendChild(meta);
    ui.listenNeighborhoodSeed.appendChild(stats);
    ui.listenNeighborhoodSeed.appendChild(hint);
    ui.listenNeighborhoodSeed.appendChild(actions);
}

function renderNeighborhoodGraph(ui, seed, artists, albums) {
    ui.listenNeighborhoodGraph.innerHTML = '';
    if (!seed || (!seed.trackId && !seed.title && !seed.artistName)) {
        ui.listenNeighborhoodGraph.appendChild(buildEmptyState('The neighborhood map appears here once you pick a seed.'));
        return;
    }

    const stage = document.createElement('div');
    stage.className = 'neighborhood-graph-stage';

    const graph = document.createElement('div');
    graph.className = 'neighborhood-graph';

    const seedNode = document.createElement('button');
    seedNode.type = 'button';
    seedNode.className = 'neighborhood-node neighborhood-node-seed active';
    seedNode.style.left = '50%';
    seedNode.style.top = '50%';
    seedNode.innerHTML = `<strong>${escapeHTML(seed.title || 'Unknown title')}</strong><span>${escapeHTML(seed.artistName || 'Unknown artist')}</span>`;
    graph.appendChild(seedNode);

    const neighbors = buildNeighborhoodNodes(artists, albums);
    neighbors.forEach((node) => {
        const edge = document.createElement('div');
        edge.className = `neighborhood-edge neighborhood-edge-${node.kind}`;
        edge.style.setProperty('--edge-length', `${node.distance}px`);
        edge.style.left = '50%';
        edge.style.top = '50%';
        edge.style.transform = `rotate(${node.angle}deg)`;
        graph.appendChild(edge);

        const button = document.createElement('button');
        button.type = 'button';
        button.className = `neighborhood-node neighborhood-node-${node.kind}`;
        button.style.left = `${node.x}%`;
        button.style.top = `${node.y}%`;
        button.innerHTML = `<strong>${escapeHTML(node.label)}</strong><span>${escapeHTML(node.subLabel)}</span>`;
        button.addEventListener('click', () => {
            if (typeof ui.handleExploreShortcut === 'function' && node.query) {
                ui.handleExploreShortcut(node.query);
            }
        });
        graph.appendChild(button);
    });

    const legend = document.createElement('div');
    legend.className = 'neighborhood-legend';
    legend.appendChild(buildNeighborhoodLegendItem('Seed track', 'seed'));
    legend.appendChild(buildNeighborhoodLegendItem('Artist branch', 'artist'));
    legend.appendChild(buildNeighborhoodLegendItem('Album branch', 'album'));

    stage.appendChild(graph);
    stage.appendChild(legend);
    ui.listenNeighborhoodGraph.appendChild(stage);
}

function buildNeighborhoodNodes(artists, albums) {
    const nodes = [];
    const artistLimit = Math.min(artists.length, 6);
    const albumLimit = Math.min(albums.length, 6);

    for (let index = 0; index < artistLimit; index += 1) {
        const item = artists[index];
        const angle = 150 + (artistLimit === 1 ? 0 : (index * 60) / Math.max(artistLimit - 1, 1));
        nodes.push(buildNeighborhoodNode(item, 'artist', angle, 34));
    }
    for (let index = 0; index < albumLimit; index += 1) {
        const item = albums[index];
        const angle = -30 + (albumLimit === 1 ? 0 : (index * 60) / Math.max(albumLimit - 1, 1));
        nodes.push(buildNeighborhoodNode(item, 'album', angle, 34));
    }

    return nodes;
}

function buildNeighborhoodNode(item, kind, angle, radiusPercent) {
    const radians = (angle * Math.PI) / 180;
    const x = 50 + Math.cos(radians) * radiusPercent;
    const y = 50 + Math.sin(radians) * radiusPercent;
    const dx = (x - 50) * 3.4;
    const dy = (y - 50) * 2.5;
    const distance = Math.sqrt((dx * dx) + (dy * dy));
    const query = kind === 'artist'
        ? (item && item.name ? String(item.name) : '')
        : [item && item.name ? String(item.name) : '', item && item.artistName ? String(item.artistName) : ''].filter(Boolean).join(' ');

    return {
        kind,
        angle,
        distance,
        x,
        y,
        label: kind === 'artist' ? (item && item.name ? String(item.name) : 'Unknown artist') : (item && item.name ? String(item.name) : 'Unknown album'),
        subLabel: kind === 'artist'
            ? buildArtistNeighborhoodMeta(item)
            : [item && item.artistName ? String(item.artistName) : '', buildAlbumNeighborhoodMeta(item)].filter(Boolean).join(' • '),
        query
    };
}

function buildNeighborhoodLegendItem(label, kind) {
    const item = document.createElement('div');
    item.className = 'neighborhood-legend-item';
    const swatch = document.createElement('span');
    swatch.className = `neighborhood-legend-swatch neighborhood-legend-swatch-${kind}`;
    const text = document.createElement('span');
    text.textContent = label;
    item.appendChild(swatch);
    item.appendChild(text);
    return item;
}

function renderClusterTask(target, task) {
    target.innerHTML = '';
    if (!task) {
        target.appendChild(buildEmptyState('No active or recent scene task.'));
        return;
    }
    target.appendChild(buildMetricGrid([
        metricItem('Task Type', task.taskType || 'unknown'),
        metricItem('Status', task.status || 'unknown'),
        metricItem('Progress', task.progress == null ? 'unknown' : `${task.progress}%`),
        metricItem('Task ID', task.taskId || 'unknown')
    ]));
}

function clusterActionLabel(clusters, canStart) {
    if (canStart) {
        return 'Start Clustering';
    }
    if (clusters && clusters.ready) {
        return 'Clusters Ready';
    }
    if (clusters && clusters.task && clusters.task.status === 'PROGRESS') {
        return 'Clustering Running';
    }
    if (clusters && clusters.configured === false) {
        return 'Sonic Disabled';
    }
    return 'Clustering Busy';
}

function renderClusterPlaylists(ui, target, playlists) {
    target.innerHTML = '';
    if (!Array.isArray(playlists) || playlists.length === 0) {
        target.appendChild(buildEmptyState('No scene playlists available yet.'));
        return;
    }
    const list = document.createElement('div');
    list.className = 'preview-result-list';
    playlists.forEach((playlist) => {
        const card = document.createElement('article');
        card.className = 'preview-result-card';
        const title = document.createElement('div');
        title.className = 'preview-result-title';
        title.textContent = playlist.name || 'Untitled cluster';
        const meta = document.createElement('div');
        meta.className = 'preview-result-meta';
        meta.textContent = `${formatInteger(playlist.songCount || 0)} songs in this scene`;
        card.appendChild(title);
        if (playlist.subtitle) {
            const subtitle = document.createElement('div');
            subtitle.className = 'preview-result-artist';
            subtitle.textContent = playlist.subtitle;
            card.appendChild(subtitle);
        }
        card.appendChild(meta);
        const actions = document.createElement('div');
        actions.className = 'map-detail-actions';
        actions.appendChild(buildMapActionButton('Inspect', () => {
            openDialog(ui, playlist.name || 'Scene Details', buildScenePlaylistDetail(ui, playlist));
        }));
        if (typeof ui.handleChatShortcut === 'function') {
            actions.appendChild(buildMapActionButton('Ask Chat', () => {
                ui.handleChatShortcut(`Build me a playlist from the scene "${playlist.name || 'Untitled cluster'}".`);
            }));
        }
        card.appendChild(actions);
        list.appendChild(card);
    });
    target.appendChild(list);
}

function buildScenePlaylistDetail(ui, playlist) {
    const wrapper = document.createElement('div');
    wrapper.className = 'overlay-stack';
    const summary = document.createElement('div');
    summary.className = 'preview-result-card';
    summary.innerHTML = `<div class="preview-result-title">${escapeHTML(playlist.name || 'Untitled cluster')}</div><div class="preview-result-meta">${escapeHTML(playlist.subtitle || `${formatInteger(playlist.songCount || 0)} songs in this scene`)}</div>`;
    wrapper.appendChild(summary);
    const songs = Array.isArray(playlist.songs) ? playlist.songs : [];
    if (songs.length === 0) {
        wrapper.appendChild(buildEmptyState('No scene tracks available.'));
        return wrapper;
    }
    const list = document.createElement('div');
    list.className = 'preview-result-list';
    songs.forEach((song, index) => {
        const row = document.createElement('article');
        row.className = 'preview-result-card';
        row.innerHTML = `<div class="preview-result-title">${index + 1}. ${escapeHTML(song.title || 'Unknown title')}</div><div class="preview-result-artist">${escapeHTML(song.author || 'Unknown artist')}</div>`;
        if (typeof ui.handleSongPathShortcut === 'function') {
            const actions = document.createElement('div');
            actions.className = 'map-detail-actions';
            const query = [song.title || '', song.author || ''].filter(Boolean).join(' ');
            if (query) {
                actions.appendChild(buildMapActionButton('Path Start', () => ui.handleSongPathShortcut('start', query)));
                actions.appendChild(buildMapActionButton('Path End', () => ui.handleSongPathShortcut('end', query)));
            }
            row.appendChild(actions);
        }
        list.appendChild(row);
    });
    wrapper.appendChild(list);
    return wrapper;
}

function renderSongPathResults(target, path) {
    target.innerHTML = '';
    if (!Array.isArray(path) || path.length === 0) {
        target.appendChild(buildEmptyState('No Song Path results yet.'));
        return;
    }
    const list = document.createElement('div');
    list.className = 'preview-result-list';
    path.forEach((item) => {
        const card = document.createElement('article');
        card.className = 'preview-result-card';

        const title = document.createElement('div');
        title.className = 'preview-result-title';
        title.textContent = `${item.position || 0}. ${item.title || 'Unknown title'}`;
        card.appendChild(title);

        const artist = document.createElement('div');
        artist.className = 'preview-result-artist';
        artist.textContent = item.artistName || 'Unknown artist';
        card.appendChild(artist);

        if (item.albumName) {
            const album = document.createElement('div');
            album.className = 'preview-result-meta';
            album.textContent = item.albumName;
            card.appendChild(album);
        }

        list.appendChild(card);
    });
    target.appendChild(list);
}

function renderMapItems(ui, target, items) {
    target.innerHTML = '';
    if (!Array.isArray(items) || items.length === 0) {
        target.appendChild(buildEmptyState('No map points available yet.'));
        return;
    }
    const mappedItems = normalizeMapPlotItems(items);
    if (mappedItems.length === 0) {
        target.appendChild(buildMapFallbackList(items));
        return;
    }

    const shell = document.createElement('div');
    shell.className = 'map-stage';

    const plot = document.createElement('div');
    plot.className = 'map-plot';
    plot.appendChild(buildMapAxisLabel('distant / sparse', 'map-axis-top'));
    plot.appendChild(buildMapAxisLabel('dense / familiar', 'map-axis-bottom'));
    plot.appendChild(buildMapAxisLabel('brighter / open', 'map-axis-left'));
    plot.appendChild(buildMapAxisLabel('darker / grounded', 'map-axis-right'));

    const detail = document.createElement('aside');
    detail.className = 'map-detail-card';
    const pointButtons = new Map();

    let activeButton = null;
    const selectItem = (item, button) => {
        if (activeButton) {
            activeButton.classList.remove('active');
        }
        activeButton = button || null;
        if (activeButton) {
            activeButton.classList.add('active');
        }
        renderMapDetail(ui, detail, item);
    };

    mappedItems.forEach((item, index) => {
        const point = document.createElement('button');
        point.type = 'button';
        point.className = 'map-point';
        point.style.left = `${item.plotX}%`;
        point.style.top = `${item.plotY}%`;
        point.style.setProperty('--map-point-color', item.color);
        point.setAttribute('aria-label', `${item.title || 'Unknown title'} by ${item.artistName || 'Unknown artist'}`);
        point.title = `${item.title || 'Unknown title'} • ${item.artistName || 'Unknown artist'}`;
        point.addEventListener('click', () => selectItem(item, point));
        pointButtons.set(item.key, point);
        plot.appendChild(point);
        if (index === 0) {
            selectItem(item, point);
        }
    });

    shell.appendChild(plot);
    shell.appendChild(detail);
    target.appendChild(shell);

    const visibleList = document.createElement('div');
    visibleList.className = 'map-visible-list';
    mappedItems.slice(0, 12).forEach((item) => {
        const card = document.createElement('button');
        card.type = 'button';
        card.className = 'map-visible-item';
        card.innerHTML = `<strong>${escapeHTML(item.title || 'Unknown title')}</strong><span>${escapeHTML(item.artistName || 'Unknown artist')}</span>`;
        card.addEventListener('click', () => {
            const matchingPoint = pointButtons.get(item.key);
            if (matchingPoint) {
                matchingPoint.click();
            } else {
                renderMapDetail(ui, detail, item);
            }
        });
        visibleList.appendChild(card);
    });
    target.appendChild(visibleList);
}

function buildMapAxisLabel(text, className) {
    const label = document.createElement('span');
    label.className = `map-axis-label ${className}`;
    label.textContent = text;
    return label;
}

function renderMapDetail(ui, target, item) {
    target.innerHTML = '';
    if (!item) {
        target.appendChild(buildEmptyState('Select a point to inspect that region of the map.'));
        return;
    }

    const eyebrow = document.createElement('div');
    eyebrow.className = 'map-detail-eyebrow';
    eyebrow.textContent = 'Focused point';

    const title = document.createElement('div');
    title.className = 'preview-result-title';
    title.textContent = item.title || 'Unknown title';

    const artist = document.createElement('div');
    artist.className = 'preview-result-artist';
    artist.textContent = item.artistName || 'Unknown artist';

    const meta = document.createElement('div');
    meta.className = 'preview-result-meta';
    meta.textContent = buildMapMeta(item);

    const hint = document.createElement('p');
    hint.className = 'listen-note';
    hint.textContent = 'Use this point to jump back into chat or to seed one side of Song Path.';

    const actions = document.createElement('div');
    actions.className = 'map-detail-actions';

    const query = [item.title || '', item.artistName || ''].filter(Boolean).join(' ');
    if (typeof ui.handleChatShortcut === 'function' && query) {
        actions.appendChild(buildMapActionButton('Ask Chat', () => {
            ui.handleChatShortcut(`What lives near "${item.title || 'this track'}" by ${item.artistName || 'this artist'} on the sonic map?`);
        }));
    }
    if (typeof ui.handleSongPathShortcut === 'function' && query) {
        actions.appendChild(buildMapActionButton('Path Start', () => {
            ui.handleSongPathShortcut('start', query);
        }));
        actions.appendChild(buildMapActionButton('Path End', () => {
            ui.handleSongPathShortcut('end', query);
        }));
    }

    target.appendChild(eyebrow);
    target.appendChild(title);
    target.appendChild(artist);
    target.appendChild(meta);
    target.appendChild(hint);
    target.appendChild(actions);
}

function buildMapActionButton(label, onClick) {
    const button = document.createElement('button');
    button.type = 'button';
    button.className = 'ghost-btn preview-result-action';
    button.textContent = label;
    button.addEventListener('click', onClick);
    return button;
}

function normalizeMapPlotItems(items) {
    const plotted = items
        .map((item, index) => {
            const hasCoords = typeof item.x === 'number' && typeof item.y === 'number';
            if (!hasCoords) {
                return null;
            }
            return {
                ...item,
                key: item.id || `${item.title || 'track'}-${item.artistName || 'artist'}-${index}`,
                index
            };
        })
        .filter(Boolean);
    if (plotted.length === 0) {
        return [];
    }

    const minX = Math.min(...plotted.map((item) => item.x));
    const maxX = Math.max(...plotted.map((item) => item.x));
    const minY = Math.min(...plotted.map((item) => item.y));
    const maxY = Math.max(...plotted.map((item) => item.y));
    const spanX = Math.max(maxX - minX, 0.001);
    const spanY = Math.max(maxY - minY, 0.001);

    return plotted.map((item, index) => {
        const normalizedX = (item.x - minX) / spanX;
        const normalizedY = (item.y - minY) / spanY;
        return {
            ...item,
            plotX: 8 + normalizedX * 84,
            plotY: 92 - normalizedY * 84,
            color: buildMapPointColor(normalizedX, normalizedY, index)
        };
    });
}

function buildMapPointColor(x, y, index) {
    const hue = Math.round((x * 220) + (y * 90) + (index * 11)) % 360;
    const saturation = 68 + Math.round(y * 18);
    const lightness = 56 - Math.round(x * 10);
    return `hsl(${hue} ${saturation}% ${lightness}%)`;
}

function buildMapFallbackList(items) {
    const shell = document.createElement('div');
    shell.className = 'preview-result-list';
    items.slice(0, 12).forEach((item, index) => {
        const card = document.createElement('article');
        card.className = 'preview-result-card';

        const title = document.createElement('div');
        title.className = 'preview-result-title';
        title.textContent = `${index + 1}. ${item.title || 'Unknown title'}`;
        card.appendChild(title);

        const artist = document.createElement('div');
        artist.className = 'preview-result-artist';
        artist.textContent = item.artistName || 'Unknown artist';
        card.appendChild(artist);

        const meta = document.createElement('div');
        meta.className = 'preview-result-meta';
        meta.textContent = buildMapMeta(item);
        card.appendChild(meta);

        shell.appendChild(card);
    });
    return shell;
}

function buildPreviewMeta(item) {
    const scores = item.sourceScores || {};
    const parts = [`score ${formatScore(item.score)}`];
    if (typeof scores.listening_affinity === 'number') {
        parts.push(`affinity ${formatScore(scores.listening_affinity)}`);
    }
    if (typeof scores.mode_adjustment === 'number' && scores.mode_adjustment !== 0) {
        parts.push(`mode ${signedScore(scores.mode_adjustment)}`);
    }
    if (typeof scores.mood_adjustment === 'number' && scores.mood_adjustment !== 0) {
        parts.push(`mood ${signedScore(scores.mood_adjustment)}`);
    }
    if (typeof scores.diversity_penalty === 'number' && scores.diversity_penalty !== 0) {
        parts.push(`diversity -${formatScore(scores.diversity_penalty)}`);
    }
    return parts.join(' • ');
}

function buildTextSearchMeta(item) {
    const parts = [];
    if (item.albumName) {
        parts.push(item.albumName);
    }
    if (typeof item.similarity === 'number') {
        parts.push(`similarity ${formatScore(item.similarity)}`);
    }
    return parts.length > 0 ? parts.join(' • ') : 'text-to-sound match';
}

function buildArtistNeighborhoodMeta(item) {
    const parts = [];
    if (typeof item.score === 'number') {
        parts.push(`score ${formatScore(item.score)}`);
    }
    if (typeof item.playCount === 'number') {
        parts.push(`${formatInteger(item.playCount)} plays`);
    }
    if (typeof item.rating === 'number' && item.rating > 0) {
        parts.push(`${item.rating} stars`);
    }
    return parts.join(' • ');
}

function buildAlbumNeighborhoodMeta(item) {
    const parts = [];
    if (typeof item.playCount === 'number') {
        parts.push(`${formatInteger(item.playCount)} plays`);
    }
    if (typeof item.rating === 'number' && item.rating > 0) {
        parts.push(`${item.rating} stars`);
    }
    if (item.year) {
        parts.push(String(item.year));
    }
    if (item.genre) {
        parts.push(String(item.genre));
    }
    return parts.join(' • ');
}

function buildNeighborhoodSeedMeta(seed) {
    const parts = [];
    if (seed.albumName) {
        parts.push(String(seed.albumName));
    }
    if (seed.albumArtistName && seed.albumArtistName !== seed.artistName) {
        parts.push(String(seed.albumArtistName));
    }
    return parts.join(' • ');
}

function buildMapMeta(item) {
    const parts = [];
    if (item.albumName) {
        parts.push(String(item.albumName));
    }
    if (typeof item.x === 'number' && typeof item.y === 'number') {
        parts.push(`x ${formatScore(item.x)} • y ${formatScore(item.y)}`);
    }
    return parts.join(' • ');
}

function formatMapTask(task) {
    if (!task) {
        return 'idle';
    }
    const type = task.taskType ? String(task.taskType) : 'task';
    const status = task.status ? String(task.status).toLowerCase() : 'unknown';
    if (typeof task.progress === 'number') {
        return `${type} ${status} ${task.progress}%`;
    }
    return `${type} ${status}`;
}

function formatDateTime(value) {
    if (!value) return '';
    const date = value instanceof Date ? value : new Date(value);
    if (Number.isNaN(date.getTime())) return '';
    return date.toLocaleString([], {
        month: 'short',
        day: 'numeric',
        hour: '2-digit',
        minute: '2-digit'
    });
}

function formatScore(value) {
    if (typeof value !== 'number' || Number.isNaN(value)) return '0.00';
    return value.toFixed(2);
}

function formatInteger(value) {
    if (typeof value !== 'number' || Number.isNaN(value)) return '0';
    return new Intl.NumberFormat().format(value);
}

function signedScore(value) {
    const sign = value >= 0 ? '+' : '';
    return sign + formatScore(value);
}

function readValue(object, ...keys) {
    for (const key of keys) {
        if (object && Object.prototype.hasOwnProperty.call(object, key)) {
            return object[key];
        }
    }
    return undefined;
}

function escapeHTML(value) {
    return String(value || '')
        .replaceAll('&', '&amp;')
        .replaceAll('<', '&lt;')
        .replaceAll('>', '&gt;')
        .replaceAll('"', '&quot;')
        .replaceAll("'", '&#39;');
}

function buildContextStatus(contextValue) {
    const mode = contextValue.mode || 'adjacent';
    const mood = contextValue.mood ? `, mood "${contextValue.mood}"` : '';
    const expires = contextValue.expiresAt ? `, expires ${formatDateTime(contextValue.expiresAt)}` : ', no expiry';
    return `Active override: ${mode}${mood}${expires}.`;
}
