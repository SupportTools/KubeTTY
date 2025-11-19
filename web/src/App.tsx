import { useState, useCallback } from "react";
import TerminalView from "./components/TerminalView";
import logo from "./assets/logo.svg";

const App = () => {
  const [toast, setToast] = useState<string | null>(null);

  const showToast = useCallback((message: string) => {
    setToast(message);
    setTimeout(() => setToast(null), 4000);
  }, []);

  const handleReconnect = useCallback(() => {
    showToast("Reconnected to shell");
  }, [showToast]);

  return (
    <div className="app-shell">
      <header className="header">
        <div className="brand">
          <img src={logo} alt="KubeTTY" className="logo" />
          <h1>KubeTTY</h1>
        </div>
      </header>
      {toast && <div className="toast">{toast}</div>}
      <section className="main full-width">
        <TerminalView onReconnect={handleReconnect} />
      </section>
    </div>
  );
};

export default App;
