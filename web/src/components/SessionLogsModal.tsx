import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { SessionLogEntry, SessionLogsResponse, SessionMeta } from "../types";
import { parseErrorResponse } from "../utils/errorParser";

type Props = {
  session: SessionMeta | null;
  onClose: () => void;
};

const decoder = new TextDecoder();
const DEBOUNCE_MS = 300;

const SessionLogsModal = ({ session, onClose }: Props) => {
  const [logs, setLogs] = useState<SessionLogEntry[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [limit, setLimit] = useState(200);
  const [search, setSearch] = useState("");
  const [debouncedSearch, setDebouncedSearch] = useState("");
  const [direction, setDirection] = useState<"" | "in" | "out">("");
  const [matchCount, setMatchCount] = useState(0);
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  // Debounce search input
  const handleSearchChange = useCallback((value: string) => {
    setSearch(value);
    if (debounceRef.current) {
      clearTimeout(debounceRef.current);
    }
    debounceRef.current = setTimeout(() => {
      setDebouncedSearch(value);
    }, DEBOUNCE_MS);
  }, []);

  // Cleanup debounce timer on unmount
  useEffect(() => {
    return () => {
      if (debounceRef.current) {
        clearTimeout(debounceRef.current);
      }
    };
  }, []);

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
        const params = new URLSearchParams({
          session: sessionId,
          limit: String(limit),
        });
        if (debouncedSearch) {
          params.set("search", debouncedSearch);
        }
        if (direction) {
          params.set("direction", direction);
        }
        const res = await fetch(`/session/logs?${params.toString()}`, {
          credentials: "include"
        });
        if (!res.ok) {
          const errorMessage = await parseErrorResponse(res);
          throw new Error(errorMessage);
        }
        const body = (await res.json()) as SessionLogsResponse;
        if (!cancelled) {
          setLogs(body.logs);
          setMatchCount(body.matchCount);
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
  }, [session?.sessionId, limit, debouncedSearch, direction]);

  const decodedLogs = useMemo(
    () =>
      logs.map((entry) => ({
        ...entry,
        text: decodePayload(entry.data),
      })),
    [logs],
  );

  // Highlight matching text in log entry
  const highlightText = useCallback((text: string) => {
    if (!debouncedSearch) {
      return text;
    }
    const regex = new RegExp(`(${escapeRegExp(debouncedSearch)})`, "gi");
    const parts = text.split(regex);
    return parts.map((part, i) =>
      regex.test(part) ? (
        <mark key={i} className="search-highlight">{part}</mark>
      ) : (
        part
      )
    );
  }, [debouncedSearch]);

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

  const handleClearSearch = () => {
    setSearch("");
    setDebouncedSearch("");
    if (debounceRef.current) {
      clearTimeout(debounceRef.current);
    }
  };

  return (
    <div className="modal-backdrop">
      <div className="modal session-logs-modal">
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
          <div className="search-container">
            <input
              type="text"
              placeholder="Search logs..."
              value={search}
              onChange={(e) => handleSearchChange(e.target.value)}
              className="search-input"
              aria-label="Search session logs"
            />
            {search && (
              <button
                className="search-clear"
                onClick={handleClearSearch}
                aria-label="Clear search"
              >
                ×
              </button>
            )}
            {debouncedSearch && (
              <span className="match-count" title="Total matching entries">
                {matchCount} {matchCount === 1 ? "match" : "matches"}
              </span>
            )}
          </div>
          <select
            value={direction}
            onChange={(e) => setDirection(e.target.value as "" | "in" | "out")}
            aria-label="Filter by direction"
          >
            <option value="">All</option>
            <option value="in">Client Input</option>
            <option value="out">Session Output</option>
          </select>
          <label>
            Show
            <select value={limit} onChange={(event) => setLimit(Number(event.target.value))}>
              <option value={50}>50</option>
              <option value={200}>200</option>
              <option value={500}>500</option>
              <option value={1000}>1000</option>
            </select>
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
              {decodedLogs.length === 0 && (debouncedSearch ? "No matching log entries found." : "No log entries recorded yet.")}
              {decodedLogs.map((entry, index) => (
                <span key={`${entry.createdAt}-${entry.direction}-${index}`}>
                  <strong>[{new Date(entry.createdAt).toLocaleTimeString()}]</strong>{" "}
                  <em>{entry.direction === "in" ? "client" : "session"}:</em> {highlightText(entry.text)}
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

// Escape special regex characters in search string
const escapeRegExp = (str: string) => {
  return str.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
};

export default SessionLogsModal;
