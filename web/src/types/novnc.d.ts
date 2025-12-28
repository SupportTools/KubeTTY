/**
 * Type declarations for @novnc/novnc
 *
 * noVNC is a JavaScript VNC client that communicates using WebSocket.
 * The RFB class is the main interface for connecting to VNC servers.
 */

declare module '@novnc/novnc/lib/rfb' {
  /**
   * Options for RFB constructor
   */
  interface RFBOptions {
    /** Credentials for authentication */
    credentials?: {
      username?: string;
      password?: string;
      target?: string;
    };
    /** Share the connection with other clients (default: true) */
    shared?: boolean;
    /** ID to send to a VNC repeater */
    repeaterID?: string;
    /** WebSocket subprotocols to use */
    wsProtocols?: string[];
  }

  /**
   * RFB capabilities object
   */
  interface RFBCapabilities {
    /** Machine power control is available */
    power: boolean;
  }

  /**
   * Event detail for 'disconnect' event
   */
  interface DisconnectEventDetail {
    /** Whether the disconnect was clean */
    clean: boolean;
  }

  /**
   * Event detail for 'credentialsrequired' event
   */
  interface CredentialsRequiredEventDetail {
    /** List of credential types required */
    types: string[];
  }

  /**
   * Event detail for 'securityfailure' event
   */
  interface SecurityFailureEventDetail {
    /** Security failure status code */
    status: number;
    /** Human-readable reason */
    reason: string;
  }

  /**
   * Event detail for 'clipboard' event
   */
  interface ClipboardEventDetail {
    /** Clipboard text content */
    text: string;
  }

  /**
   * Event detail for 'desktopname' event
   */
  interface DesktopNameEventDetail {
    /** The new desktop name */
    name: string;
  }

  /**
   * Event detail for 'serververification' event
   */
  interface ServerVerificationEventDetail {
    /** Type of verification needed */
    type: string;
    /** Verification data */
    publickey?: string;
  }

  /**
   * RFB custom events
   */
  interface RFBEventMap {
    connect: CustomEvent<void>;
    disconnect: CustomEvent<DisconnectEventDetail>;
    credentialsrequired: CustomEvent<CredentialsRequiredEventDetail>;
    securityfailure: CustomEvent<SecurityFailureEventDetail>;
    clipboard: CustomEvent<ClipboardEventDetail>;
    bell: CustomEvent<void>;
    desktopname: CustomEvent<DesktopNameEventDetail>;
    capabilities: CustomEvent<void>;
    clippingviewport: CustomEvent<void>;
    serververification: CustomEvent<ServerVerificationEventDetail>;
  }

  /**
   * RFB - Remote Frame Buffer protocol client
   *
   * The RFB object represents a single connection to a VNC server.
   * It communicates using a WebSocket that provides a standard RFB protocol stream.
   */
  class RFB {
    /**
     * Creates a new RFB connection
     * @param target - DOM element where the VNC canvas will be attached
     * @param urlOrChannel - WebSocket URL or existing WebSocket/RTCDataChannel
     * @param options - Connection options
     */
    constructor(
      target: HTMLElement,
      urlOrChannel: string | WebSocket | RTCDataChannel,
      options?: RFBOptions
    );

    // Properties
    /** Background style for the canvas container */
    background: string;
    /** Available server capabilities (read-only) */
    readonly capabilities: RFBCapabilities;
    /** Whether the viewport is currently clipped (read-only) */
    readonly clippingViewport: boolean;
    /** Enable clipping of remote session to container */
    clipViewport: boolean;
    /** Compression level 0-9 (default: 2) */
    compressionLevel: number;
    /** Enable dragging to scroll clipped viewport */
    dragViewport: boolean;
    /** Auto-focus on click/touch (default: true) */
    focusOnClick: boolean;
    /** JPEG quality level 0-9 (default: 6) */
    qualityLevel: number;
    /** Request server to resize to match container */
    resizeSession: boolean;
    /** Scale remote session to fit container */
    scaleViewport: boolean;
    /** Show dot cursor for invisible cursors */
    showDotCursor: boolean;
    /** Prevent sending events to server */
    viewOnly: boolean;

    // Methods
    /** Approve server identity after serververification event */
    approveServer(): void;
    /** Remove keyboard focus */
    blur(): void;
    /** Send clipboard data to server */
    clipboardPasteFrom(text: string): void;
    /** Disconnect from VNC server */
    disconnect(): void;
    /** Give keyboard focus */
    focus(): void;
    /** Get current screen content as ImageData */
    getImageData(): ImageData;
    /** Request machine reboot */
    machineReboot(): void;
    /** Request machine reset */
    machineReset(): void;
    /** Request machine shutdown */
    machineShutdown(): void;
    /** Send credentials after credentialsrequired event */
    sendCredentials(credentials: { username?: string; password?: string; target?: string }): void;
    /** Send Ctrl+Alt+Del key sequence */
    sendCtrlAltDel(): void;
    /** Send a key event */
    sendKey(keysym: number, code: string | null, down?: boolean): void;
    /** Get screen content as Blob */
    toBlob(callback: (blob: Blob | null) => void, type?: string, quality?: number): void;
    /** Get screen content as data URL */
    toDataURL(type?: string, encoderOptions?: number): string;

    // Event handlers
    addEventListener<K extends keyof RFBEventMap>(
      type: K,
      listener: (event: RFBEventMap[K]) => void,
      options?: boolean | AddEventListenerOptions
    ): void;
    removeEventListener<K extends keyof RFBEventMap>(
      type: K,
      listener: (event: RFBEventMap[K]) => void,
      options?: boolean | EventListenerOptions
    ): void;
  }

  export default RFB;
}
