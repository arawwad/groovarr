import { addMessage, appendRichText, isNearScrollBottom, resizeComposer, scheduleScrollToBottom, setPendingActionResolutionState, setRequestInFlight, setStatus, trimRenderedMessages } from './dom.js';
import { historyFromMessages, pushPersistedMessage, saveState, updatePendingAction } from './storage.js';

function buildRequest(state, message, stream) {
    return {
        message,
        history: historyFromMessages(state.messages),
        model: state.model || undefined,
        stream,
        sessionId: state.sessionId
    };
}

async function parseStream(response, handlers) {
    const reader = response.body.getReader();
    const decoder = new TextDecoder();
    let buffer = '';

    while (true) {
        const { done, value } = await reader.read();
        if (done) break;
        buffer += decoder.decode(value, { stream: true });

        let boundary = buffer.indexOf('\n\n');
        while (boundary !== -1) {
            const rawEvent = buffer.slice(0, boundary);
            buffer = buffer.slice(boundary + 2);
            boundary = buffer.indexOf('\n\n');

            const dataLine = rawEvent
                .split('\n')
                .find((line) => line.startsWith('data: '));
            if (!dataLine) continue;

            let payload = null;
            try {
                payload = JSON.parse(dataLine.slice(6));
            } catch (_) {
                continue;
            }

            if (payload.type === 'delta' && typeof handlers.onDelta === 'function') {
                handlers.onDelta(payload.delta || '');
            } else if (payload.type === 'done' && typeof handlers.onDone === 'function') {
                handlers.onDone(payload);
            } else if (payload.type === 'error' && typeof handlers.onError === 'function') {
                handlers.onError(payload.error || 'Stream failed');
            }
        }
    }
}

export async function loadModels(ui, state) {
    try {
        const response = await fetch('/api/chat/models');
        if (!response.ok) throw new Error('Failed to load models');
        const payload = await response.json();
        ui.modelSelect.innerHTML = '';
        const models = Array.isArray(payload.models) ? payload.models : [];
        models.forEach((model) => {
            const option = document.createElement('option');
            option.value = model;
            option.textContent = model;
            ui.modelSelect.appendChild(option);
        });
        const selected = state.model || payload.defaultModel || models[0] || '';
        if (selected) {
            ui.modelSelect.value = selected;
            state.model = selected;
            saveState(state);
        }
    } catch (_) {
        setStatus(ui.agentStatus, 'Agent Models Unavailable', 'offline');
    }
}

export function renderPersistedMessages({ ui, state, onResolvePendingAction, maxRenderedMessages }) {
    ui.chat.innerHTML = '';
    state.messages.forEach((message) => {
        addMessage({
            chat: ui.chat,
            role: message.role,
            content: message.content,
            extraClass: message.extraClass,
            metaLabel: message.metaLabel,
            pendingAction: message.pendingAction || null,
            onResolvePendingAction,
            onAfterRender: () => trimRenderedMessages(ui.chat, maxRenderedMessages)
        });
    });
    scheduleScrollToBottom(ui.chat);
}

export async function sendChatMessage({ ui, state, text, onResolvePendingAction, maxRenderedMessages, runtime }) {
    const message = text.trim();
    if (!message || runtime.activeRequest) return;

    addMessage({
        chat: ui.chat,
        role: 'user',
        content: message,
        onAfterRender: () => {
            trimRenderedMessages(ui.chat, maxRenderedMessages);
            scheduleScrollToBottom(ui.chat);
        }
    });
    pushPersistedMessage(state, { role: 'user', content: message });

    ui.input.value = '';
    ui.input.focus();
    resizeComposer(ui.input);
    setRequestInFlight(ui, true);
    setStatus(ui.agentStatus, 'Agent Thinking', 'busy');

    const loadingNode = addMessage({
        chat: ui.chat,
        role: 'assistant',
        content: '',
        extraClass: 'loading',
        metaLabel: 'Assistant',
        onAfterRender: () => {
            trimRenderedMessages(ui.chat, maxRenderedMessages);
            scheduleScrollToBottom(ui.chat);
        }
    });
    const body = loadingNode.querySelector('.body');
    let finalResponse = '';
    let renderedResponse = '';
    let renderFrameId = 0;
    const controller = new AbortController();
    runtime.activeRequest = controller;

    const flushStreamingBody = () => {
        renderFrameId = 0;
        if (renderedResponse === finalResponse) return;
        const shouldStick = isNearScrollBottom(ui.chat);
        renderedResponse = finalResponse;
        loadingNode.classList.remove('loading');
        loadingNode.classList.add('streaming');
        body.textContent = renderedResponse;
        if (shouldStick) {
            scheduleScrollToBottom(ui.chat);
        }
    };

    const scheduleStreamingRender = () => {
        if (renderFrameId) return;
        renderFrameId = window.requestAnimationFrame(flushStreamingBody);
    };

    try {
        const response = await fetch('/api/chat', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(buildRequest(state, message, true)),
            signal: controller.signal
        });
        if (!response.ok || !response.body) {
            throw new Error('Chat request failed');
        }

        await parseStream(response, {
            onDelta(delta) {
                finalResponse += delta;
                scheduleStreamingRender();
            },
            onDone(payload) {
                if (renderFrameId) {
                    window.cancelAnimationFrame(renderFrameId);
                    flushStreamingBody();
                }
                const shouldStick = isNearScrollBottom(ui.chat);
                finalResponse = payload.response || finalResponse;
                loadingNode.classList.remove('loading', 'streaming');
                appendRichText(body, finalResponse);
                const pendingAction = payload.pendingAction || null;
                if (pendingAction) {
                    const rendered = addMessage({
                        chat: document.createDocumentFragment(),
                        role: 'assistant',
                        content: '',
                        pendingAction,
                        onResolvePendingAction
                    });
                    const card = rendered.querySelector('.pending-action');
                    if (card) loadingNode.appendChild(card);
                }
                if (shouldStick) {
                    scheduleScrollToBottom(ui.chat);
                }
                pushPersistedMessage(state, {
                    role: 'assistant',
                    content: finalResponse,
                    pendingAction
                });
            },
            onError(messageText) {
                throw new Error(messageText);
            }
        });
        setStatus(ui.agentStatus, 'Agent Online', '');
    } catch (error) {
        loadingNode.classList.remove('loading', 'streaming');
        if (renderFrameId) {
            window.cancelAnimationFrame(renderFrameId);
            renderFrameId = 0;
        }
        if (error && error.name === 'AbortError') {
            const stoppedMessage = finalResponse
                ? finalResponse + '\n\n- Response stopped.'
                : 'Response stopped.';
            loadingNode.classList.add('event');
            appendRichText(body, stoppedMessage);
            pushPersistedMessage(state, {
                role: 'assistant',
                content: stoppedMessage,
                extraClass: 'event'
            });
            setStatus(ui.agentStatus, 'Agent Online', '');
        } else {
            loadingNode.classList.add('error');
            body.textContent = error && error.message ? error.message : 'Failed to process query.';
            pushPersistedMessage(state, {
                role: 'assistant',
                content: body.textContent,
                extraClass: 'error'
            });
            setStatus(ui.agentStatus, 'Agent Offline', 'offline');
        }
    } finally {
        runtime.activeRequest = null;
        setRequestInFlight(ui, false);
        trimRenderedMessages(ui.chat, maxRenderedMessages);
    }
}

export async function resolvePendingAction({ ui, state, actionId, command, card, maxRenderedMessages }) {
    try {
        const response = await fetch(`/api/pending-actions/${encodeURIComponent(actionId)}/${encodeURIComponent(command)}`, {
            method: 'POST'
        });
        const payload = await response.json();
        if (!response.ok) {
            throw new Error(payload.error || 'Pending action failed');
        }
        updatePendingAction(state, actionId);
        setPendingActionResolutionState(card, command);
        addMessage({
            chat: ui.chat,
            role: 'assistant',
            content: payload.response || (command === 'discard' ? 'Request discarded.' : 'Done.'),
            metaLabel: 'Assistant',
            onAfterRender: () => {
                trimRenderedMessages(ui.chat, maxRenderedMessages);
                scheduleScrollToBottom(ui.chat);
            }
        });
        pushPersistedMessage(state, {
            role: 'assistant',
            content: payload.response || (command === 'discard' ? 'Request discarded.' : 'Done.')
        });
    } catch (error) {
        addMessage({
            chat: ui.chat,
            role: 'assistant',
            content: error && error.message ? error.message : 'Pending action failed.',
            extraClass: 'error',
            metaLabel: 'Assistant',
            onAfterRender: () => {
                trimRenderedMessages(ui.chat, maxRenderedMessages);
                scheduleScrollToBottom(ui.chat);
            }
        });
    }
}
