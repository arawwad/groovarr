import { resizeComposer, resetEventLog, trimRenderedMessages } from './dom.js';
import { resetState, saveState } from './storage.js';

export function bindModelControls({ ui, state }) {
    ui.modelSelect.addEventListener('change', () => {
        state.model = ui.modelSelect.value;
        saveState(state);
    });

    ui.muteEvents.addEventListener('change', () => {
        state.muteEvents = ui.muteEvents.checked;
        saveState(state);
    });
}

export function bindComposer({ ui, state, sendCurrentInput, stopCurrentInput }) {
    ui.sendButton.addEventListener('click', sendCurrentInput);
    ui.stopButton.addEventListener('click', stopCurrentInput);

    ui.input.addEventListener('keydown', (event) => {
        if (event.key === 'Enter' && !event.shiftKey) {
            event.preventDefault();
            sendCurrentInput();
        }
    });

    ui.input.addEventListener('input', () => {
        resizeComposer(ui.input);
    });

    resizeComposer(ui.input);
}

export function bindClearChat({ ui, state, maxRenderedMessages, onCleared }) {
    ui.clearButton.addEventListener('click', () => {
        resetState(state);
        ui.chat.innerHTML = '';
        ui.input.value = '';
        ui.muteEvents.checked = false;
        resizeComposer(ui.input);
        trimRenderedMessages(ui.chat, maxRenderedMessages);
        if (typeof onCleared === 'function') onCleared();
    });
}

export function bindClearEvents({ ui }) {
    ui.clearEventsButton.addEventListener('click', () => {
        resetEventLog(ui.eventsLog);
    });
}

export function bindActivityDrawer({ ui, onToggle, onClose }) {
    ui.activityToggleButton.addEventListener('click', onToggle);
    ui.closeActivityButton.addEventListener('click', onClose);
    ui.activityBackdrop.addEventListener('click', onClose);
}

export function bindMobileOptions({ ui, onToggle }) {
    ui.optionsToggleButton.addEventListener('click', onToggle);
}
