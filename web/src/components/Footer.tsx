interface FooterProps {
  version?: string;
}

const Footer = ({ version = '0.5.1' }: FooterProps) => {
  return (
    <footer className="app-footer">
      <span className="footer-version">KubeTTY v{version}</span>
      <nav className="footer-links">
        <a
          href="https://github.com/supporttools/KubeTTY#readme"
          target="_blank"
          rel="noopener noreferrer"
        >
          Docs
        </a>
        <span className="footer-separator">|</span>
        <a
          href="https://github.com/supporttools/KubeTTY"
          target="_blank"
          rel="noopener noreferrer"
        >
          GitHub
        </a>
        <span className="footer-separator">|</span>
        <a
          href="https://github.com/supporttools/KubeTTY/issues"
          target="_blank"
          rel="noopener noreferrer"
        >
          Issues
        </a>
      </nav>
      <span className="footer-copyright">Support.Tools</span>
    </footer>
  );
};

export default Footer;
