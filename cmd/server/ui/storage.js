const STORAGE_KEY = 'groovarr-ui-state';
const MAX_PERSISTED_MESSAGES = 40;

function newSessionId() {
    if (globalThis.crypto && typeof globalThis.crypto.randomUUID === 'function') {
        return globalThis.crypto.randomUUID();
    }
    return 'sess-' + Date.now().toString(36) + '-' + Math.random().toString(36).slice(2, 10);
}

function baseState() {
    return {
        sessionId: newSessionId(),
        model: '',
        muteEvents: false,
        messages: []
    };
}

export function loadState() {
    try {
        const raw = window.localStorage.getItem(STORAGE_KEY);
        if (!raw) return baseState();
        const parsed = JSON.parse(raw);
        return {
            sessionId: typeof parsed.sessionId === 'string' && parsed.sessionId ? parsed.sessionId : newSessionId(),
            model: typeof parsed.model === 'string' ? parsed.model : '',
            muteEvents: Boolean(parsed.muteEvents),
            messages: Array.isArray(parsed.messages) ? parsed.messages.slice(-MAX_PERSISTED_MESSAGES) : []
        };
    } catch (_) {
        return baseState();
    }
}

export function saveState(state) {
    const safeState = {
        sessionId: state.sessionId,
        model: state.model || '',
        muteEvents: Boolean(state.muteEvents),
        messages: Array.isArray(state.messages) ? state.messages.slice(-MAX_PERSISTED_MESSAGES) : []
    };
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify(safeState));
}

export function resetState(state) {
    const next = baseState();
    state.sessionId = next.sessionId;
    state.model = '';
    state.muteEvents = false;
    state.messages = [];
    saveState(state);
}

export function pushPersistedMessage(state, message) {
    state.messages.push(message);
    if (state.messages.length > MAX_PERSISTED_MESSAGES) {
        state.messages = state.messages.slice(-MAX_PERSISTED_MESSAGES);
    }
    saveState(state);
}

export function updatePendingAction(state, actionId) {
    let changed = false;
    state.messages = state.messages.map((message) => {
        if (!message.pendingAction || message.pendingAction.id !== actionId) {
            return message;
        }
        changed = true;
        return { ...message, pendingAction: null };
    });
    if (changed) saveState(state);
}

export function historyFromMessages(messages) {
    return messages
        .filter((message) => message && (message.role === 'user' || message.role === 'assistant'))
        .map((message) => ({
            role: message.role,
            content: typeof message.content === 'string' ? message.content : ''
        }))
        .filter((message) => message.content.trim() !== '');
}
