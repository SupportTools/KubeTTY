import { useEffect, useMemo, useState } from "react";
import type { SessionLogEntry, SessionLogsResponse, SessionMeta } from "../types";

type Props = {
  session: SessionMeta | null;
  onClose: () => void;
};

const decoder = new TextDecoder();

const SessionLogsModal = ({ session, onClose }: Props) => {
  const [logs, setLogs] = useState<SessionLogEntry[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [limit, setLimit] = useState(200);

  useEffect(() => {
    const sessionId = session?.sessionId;
    if (!sessionId) {
      return;
    }
    let cancelled = false;
    const fetchLogs = async () => {
      setLoading(true);
      setError(null);
      try {
        const res = await fetch(`/session/logs?session=${sessionId}&limit=${limit}`);
        if (!res.ok) {
          throw new Error(await res.text());
        }
        const body = (await res.json()) as SessionLogsResponse;
        if (!cancelled) {
          setLogs(body.logs);
        }
      } catch (err) {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : String(err));
        }
      } finally {
        if (!cancelled) {
          setLoading(false);
        }
      }
    };
    fetchLogs();
    return () => {
      cancelled = true;
    };
  }, [session?.sessionId, limit]);

  const decodedLogs = useMemo(
    () =>
      logs.map((entry) => ({
        ...entry,
        text: decodePayload(entry.data),
      })),
    [logs],
  );

  if (!session) {
    return null;
  }

  const handleDownload = () => {
    if (!decodedLogs.length) {
      return;
    }
    const contents = decodedLogs
      .map((entry) => {
        const ts = new Date(entry.createdAt).toISOString();
        return `[${ts}] ${entry.direction.toUpperCase()} ${entry.text}`;
      })
      .join("\n");
    const blob = new Blob([contents], { type: "text/plain" });
    const link = document.createElement("a");
    link.href = URL.createObjectURL(blob);
    link.download = `${session.sessionId}-logs.txt`;
    link.click();
    URL.revokeObjectURL(link.href);
  };

  return (
    <div className="modal-backdrop">
      <div className="modal">
        <header className="modal-header">
          <div>
            <p className="eyebrow">Session Transcript</p>
            <h3>{session.sessionId}</h3>
          </div>
          <button className="icon-button" aria-label="Close" onClick={onClose}>
            ×
          </button>
        </header>
        <div className="modal-toolbar">
          <label>
            Show last
            <select value={limit} onChange={(event) => setLimit(Number(event.target.value))}>
              <option value={50}>50</option>
              <option value={200}>200</option>
              <option value={500}>500</option>
              <option value={1000}>1000</option>
            </select>
            events
          </label>
          <div className="spacer" />
          <button className="secondary" onClick={handleDownload} disabled={!decodedLogs.length}>
            Download
          </button>
        </div>
        <div className="modal-body">
          {loading && <p>Loading logs…</p>}
          {error && <p className="error">{error}</p>}
          {!loading && !error && (
            <pre className="log-stream">
              {decodedLogs.length === 0 && "No log entries recorded yet."}
              {decodedLogs.map((entry, index) => (
                <span key={`${entry.createdAt}-${entry.direction}-${index}`}>
                  <strong>[{new Date(entry.createdAt).toLocaleTimeString()}]</strong>{" "}
                  <em>{entry.direction === "in" ? "client" : "session"}:</em> {entry.text}
                  {"\n"}
                </span>
              ))}
            </pre>
          )}
        </div>
      </div>
    </div>
  );
};

const decodePayload = (payload: string) => {
  try {
    const binary = atob(payload);
    const bytes = new Uint8Array(binary.length);
    for (let i = 0; i < binary.length; i += 1) {
      bytes[i] = binary.charCodeAt(i);
    }
    return decoder.decode(bytes);
  } catch {
    return "[binary]";
  }
};

export default SessionLogsModal;
