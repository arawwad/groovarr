import { closeDialog, closeSheet, getUI, openDialog, openSheet, refreshMobileStatus, renderChatEmptyState, resizeComposer, setActivityBadge, setActivityOpen, setConversationIdle, setCurrentView, setExploreMode, setOptionsOpen, setRequestInFlight, setStatus, showToast } from './dom.js';
import { attachEventStream } from './events.js';
import { bindActivityDrawer, bindClearChat, bindClearEvents, bindComposer, bindMobileOptions, bindModelControls } from './bindings.js';
import { loadModels, renderPersistedMessages, resolvePendingAction, sendChatMessage } from './chat.js';
import { bindListenControls, loadExploreOverview, loadListenOverview } from './listening.js';
import { loadState } from './storage.js';

const MAX_RENDERED_MESSAGES = 60;
const MAX_RENDERED_EVENTS = 80;
const EMPTY_PROMPTS = [
    'Recommend a late-night album from my library',
    'Find artists similar to Sade',
    'What are the highest-rated albums I have not heard yet?'
];

export function startApp() {
    const ui = getUI();
    const state = loadState();
    const runtime = { activeRequest: null, activityOpen: false, optionsOpen: false, unreadEvents: 0, currentView: 'chat', exploreMode: 'vibe' };

    ui.muteEvents.checked = state.muteEvents;
    ui.showToast = (message, variant, timeoutMs) => showToast(ui, message, variant, timeoutMs);
    ui.openSheet = (title, content) => openSheet(ui, title, content);
    ui.closeSheet = () => closeSheet(ui);
    ui.openDialog = (title, content) => openDialog(ui, title, content);
    ui.closeDialog = () => closeDialog(ui);
    closeSheet(ui);
    closeDialog(ui);
    resizeComposer(ui.input);
    setRequestInFlight(ui, false);
    setActivityOpen(ui, false);
    setActivityBadge(ui, 0);
    setOptionsOpen(ui, false);
    window.addEventListener('groovarr:status-change', () => refreshMobileStatus(ui));

    const isDesktop = () => window.matchMedia('(min-width: 860px)').matches;
    const routeFromLocation = () => {
        if (window.location.pathname.startsWith('/listen')) return 'listen';
        if (window.location.pathname.startsWith('/explore')) return 'explore';
        return 'chat';
    };

    const applyActivityState = (open) => {
        runtime.activityOpen = open;
        setActivityOpen(ui, open);
        if (open) {
            runtime.unreadEvents = 0;
            setActivityBadge(ui, 0);
            ui.eventsLog.scrollTop = ui.eventsLog.scrollHeight;
        }
    };

    const renderEmptyConversation = () => {
        setConversationIdle(ui, true);
        renderChatEmptyState({
            chat: ui.chat,
            prompts: EMPTY_PROMPTS,
            onPromptSelect: (prompt) => {
                ui.input.value = prompt;
                resizeComposer(ui.input);
                ui.input.focus();
            }
        });
    };

    const handleResolvePendingAction = (actionId, command, card) => {
        resolvePendingAction({
            ui,
            state,
            actionId,
            command,
            card,
            maxRenderedMessages: MAX_RENDERED_MESSAGES
        });
    };

    const sendCurrentInput = () => {
        if (!ui.input.value.trim() || runtime.activeRequest) return;
        setConversationIdle(ui, false);
        if (!isDesktop()) {
            runtime.optionsOpen = false;
            setOptionsOpen(ui, false);
        }
        sendChatMessage({
            ui,
            state,
            runtime,
            text: ui.input.value,
            onResolvePendingAction: handleResolvePendingAction,
            maxRenderedMessages: MAX_RENDERED_MESSAGES
        });
    };

    const stopCurrentInput = () => {
        if (runtime.activeRequest) {
            runtime.activeRequest.abort();
        }
    };

    const toggleActivity = () => {
        if (isDesktop()) return;
        applyActivityState(!runtime.activityOpen);
    };

    const closeActivity = () => {
        if (!runtime.activityOpen) return;
        applyActivityState(false);
    };

    const toggleOptions = () => {
        if (isDesktop()) return;
        runtime.optionsOpen = !runtime.optionsOpen;
        setOptionsOpen(ui, runtime.optionsOpen);
    };

    const showView = async (view, pushHistory = false) => {
        runtime.currentView = view;
        setCurrentView(ui, view);
        if (pushHistory) {
            const nextPath = view === 'listen' ? '/listen' : view === 'explore' ? '/explore' : '/';
            if (window.location.pathname !== nextPath) {
                window.history.pushState({ view }, '', nextPath);
            }
        }
        if (view === 'listen') {
            runtime.optionsOpen = false;
            setOptionsOpen(ui, false);
            applyActivityState(false);
            await loadListenOverview(ui);
            return;
        }
        if (view === 'explore') {
            runtime.optionsOpen = false;
            setOptionsOpen(ui, false);
            applyActivityState(false);
            setExploreMode(ui, runtime.exploreMode);
            await loadExploreOverview(ui);
            return;
        }
        if (ui.input && document.activeElement !== ui.input) {
            ui.input.focus();
        }
    };

    ui.handleChatShortcut = async (prompt) => {
        const text = String(prompt || '').trim();
        if (!text || !ui.input) return;
        await showView('chat', true);
        ui.input.value = text;
        resizeComposer(ui.input);
        ui.input.focus();
        const end = ui.input.value.length;
        if (typeof ui.input.setSelectionRange === 'function') {
            ui.input.setSelectionRange(end, end);
        }
    };

    renderPersistedMessages({
        ui,
        state,
        onResolvePendingAction: handleResolvePendingAction,
        maxRenderedMessages: MAX_RENDERED_MESSAGES
    });
    if (state.messages.length === 0) {
        renderEmptyConversation();
    } else {
        setConversationIdle(ui, false);
    }

    loadModels(ui, state);
    bindModelControls({ ui, state });
    bindComposer({ ui, state, sendCurrentInput, stopCurrentInput });
    bindClearChat({
        ui,
        state,
        maxRenderedMessages: MAX_RENDERED_MESSAGES,
        onCleared: renderEmptyConversation
    });
    bindClearEvents({ ui });
    bindActivityDrawer({ ui, onToggle: toggleActivity, onClose: closeActivity });
    bindMobileOptions({ ui, onToggle: toggleOptions });
    bindListenControls(ui);
    if (ui.listenDiagnosticsButton) {
        ui.listenDiagnosticsButton.addEventListener('click', () => {
            if (typeof ui.openDialog !== 'function') return;
            const content = document.createElement('div');
            content.className = 'overlay-stack';
            [ui.listenSimilarityCard, ui.listenSyncCard, ui.listenTasteCard].forEach((card) => {
                if (!card) return;
                const clone = card.cloneNode(true);
                clone.hidden = false;
                clone.removeAttribute('id');
                content.appendChild(clone);
            });
            ui.openDialog('System Details', content);
        });
    }
    if (ui.listenPreviewDetailsButton) {
        ui.listenPreviewDetailsButton.addEventListener('click', () => {
            if (ui.listenPreviewDetailsButton.disabled || typeof ui.openDialog !== 'function' || !ui.listenPreviewDetails) {
                return;
            }
            const content = ui.listenPreviewDetails.cloneNode(true);
            content.hidden = false;
            content.removeAttribute('id');
            ui.openDialog('Instant Mix Comparison', content);
        });
    }
    if (Array.isArray(ui.exploreModeButtons)) {
        ui.exploreModeButtons.forEach((button) => {
            button.addEventListener('click', () => {
                runtime.exploreMode = button.dataset.exploreMode || 'vibe';
                closeSheet(ui);
                closeDialog(ui);
                setExploreMode(ui, runtime.exploreMode);
            });
        });
    }
    if (ui.appSheetClose) {
        ui.appSheetClose.addEventListener('click', () => closeSheet(ui));
    }
    if (ui.appSheetBackdrop) {
        ui.appSheetBackdrop.addEventListener('click', () => closeSheet(ui));
    }
    if (ui.appDialogClose) {
        ui.appDialogClose.addEventListener('click', () => closeDialog(ui));
    }
    if (ui.appDialogBackdrop) {
        ui.appDialogBackdrop.addEventListener('click', () => closeDialog(ui));
    }
    if (ui.appDialog) {
        ui.appDialog.addEventListener('click', (event) => {
            const target = event.target instanceof Element ? event.target.closest('[data-dialog-close]') : null;
            if (target) {
                closeDialog(ui);
            }
        });
    }
    window.addEventListener('keydown', (event) => {
        if (event.key !== 'Escape') return;
        closeSheet(ui);
        closeDialog(ui);
    });
    ui.routeLinks.forEach((link) => {
        link.addEventListener('click', (event) => {
            event.preventDefault();
            const route = link.dataset.route === 'listen' ? 'listen' : link.dataset.route === 'explore' ? 'explore' : 'chat';
            showView(route, true);
        });
    });
    ui.refreshListenButton.addEventListener('click', () => {
        loadListenOverview(ui);
    });
    if (ui.refreshExploreButton) {
        ui.refreshExploreButton.addEventListener('click', () => {
            loadExploreOverview(ui);
        });
    }

    window.addEventListener('resize', () => {
        if (isDesktop()) {
            applyActivityState(false);
            runtime.optionsOpen = false;
            setOptionsOpen(ui, false);
            ui.clearButton.hidden = false;
        }
    });
    window.addEventListener('popstate', () => {
        showView(routeFromLocation(), false);
    });

    attachEventStream({
        ui,
        state,
        onEventAdded: () => {
            if (runtime.activityOpen || isDesktop()) return;
            runtime.unreadEvents += 1;
            setActivityBadge(ui, runtime.unreadEvents);
        },
        maxRenderedMessages: MAX_RENDERED_EVENTS
    });

    setStatus(ui.agentStatus, 'Agent Online', '');
    refreshMobileStatus(ui);
    showView(routeFromLocation(), false);
}
