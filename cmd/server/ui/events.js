import { addEventEntry, isNearScrollBottom, scheduleScrollToBottom, setStatus, trimRenderedMessages } from './dom.js';

export function attachEventStream({ ui, state, onStateChange, onEventAdded, maxRenderedMessages }) {
    let source = null;

    const connect = () => {
        if (source) source.close();
        setStatus(ui.eventsStatus, 'Events Connecting', 'busy');
        source = new EventSource('/api/events');

        source.onopen = () => {
            setStatus(ui.eventsStatus, 'Events Online', '');
        };

        source.onerror = () => {
            setStatus(ui.eventsStatus, 'Events Offline', 'offline');
        };

        source.onmessage = (event) => {
            let payload = null;
            try {
                payload = JSON.parse(event.data);
            } catch (_) {
                return;
            }
            if (!payload || !payload.message) return;
            if (payload.type === 'system') return;
            if (state.muteEvents) return;

            const shouldStick = isNearScrollBottom(ui.eventsLog);
            addEventEntry({
                eventsLog: ui.eventsLog,
                content: payload.message,
                metaLabel: 'Event',
                onAfterRender: () => {
                    trimRenderedMessages(ui.eventsLog, maxRenderedMessages);
                    if (shouldStick) {
                        scheduleScrollToBottom(ui.eventsLog);
                    }
                }
            });
            if (typeof onEventAdded === 'function') onEventAdded(payload);
            if (typeof onStateChange === 'function') onStateChange();
        };
    };

    connect();
    return () => {
        if (source) source.close();
    };
}
