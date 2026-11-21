import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import AdminProjectDetail from "./AdminProjectDetail";
import { AdminProject } from "../types";

// cluster-services-1024: pending changes display and the
// "apply on next restart" save mode.

const authFetch = vi.fn();
vi.mock("../contexts/AuthContext", () => ({
  useAuth: () => ({ authFetch }),
}));

const okJSON = (body: unknown) =>
  Promise.resolve(new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } }));

function project(overrides: Partial<AdminProject> = {}): AdminProject {
  return {
    id: "11111111-1111-1111-1111-111111111111",
    name: "demo",
    displayName: "Demo",
    targetNamespace: "kubetty-demo",
    sessionId: "22222222-2222-2222-2222-222222222222",
    userName: "tester",
    cpuRequest: "100m",
    cpuLimit: "500m",
    memoryRequest: "128Mi",
    memoryLimit: "512Mi",
    storageSize: "10Gi",
    storageClass: "standard",
    adminNamespaces: [],
    readNamespaces: [],
    maxTabsPerClient: 5,
    maxTabsTotal: 10,
    sessionMode: "exclusive_takeover",
    dindEnabled: true,
    guiEnabled: false,
    ptyLoggingEnabled: false,
    imageRepository: "harbor.support.tools/kubetty/kubetty",
    imageTag: "v1.0.0",
    status: "running",
    paused: false,
    createdAt: "2026-10-01T00:00:00Z",
    updatedAt: "2026-10-01T00:00:00Z",
    ...overrides,
  };
}

// The component polls /status and replaces its project with the response, so
// the mock serves the same project back.
function renderDetail(p: AdminProject) {
  authFetch.mockImplementation((url: string) =>
    url.endsWith("/status")
      ? okJSON({ project: p, deployment: { exists: true, replicas: 1, readyReplicas: 1, availableReplicas: 1, pods: [] } })
      : okJSON(p)
  );
  return render(
    <AdminProjectDetail
      project={p}
      onClose={() => {}}
      onRefresh={() => {}}
      onDelete={() => {}}
      onRestart={() => {}}
    />
  );
}

function putBodies(): Record<string, unknown>[] {
  return authFetch.mock.calls
    .filter(([, init]) => init && (init as RequestInit).method === "PUT")
    .map(([, init]) => JSON.parse((init as RequestInit).body as string));
}

describe("AdminProjectDetail pending changes", () => {
  beforeEach(() => {
    authFetch.mockReset();
  });

  it("shows pending changes with running and next-restart values", () => {
    renderDetail(
      project({
        pendingChanges: [{ field: "cpuLimit", running: "500m", desired: "2" }],
        driftCheckedAt: "2026-10-29T12:00:00Z",
      })
    );
    expect(screen.getByText("Pending Changes")).toBeInTheDocument();
    expect(screen.getByText("cpuLimit")).toBeInTheDocument();
    expect(screen.getByText("2")).toBeInTheDocument();
    expect(screen.getByText(/nothing restarts automatically/)).toBeInTheDocument();
  });

  it("hides the section when there is no drift", () => {
    renderDetail(project());
    expect(screen.queryByText("Pending Changes")).not.toBeInTheDocument();
  });

  it("saves with applyMode now by default", async () => {
    renderDetail(project());
    fireEvent.click(screen.getByText("Edit Resources"));
    fireEvent.change(screen.getByLabelText("CPU Limit:"), { target: { value: "2" } });
    fireEvent.click(screen.getByText("Save Changes"));
    await waitFor(() => expect(putBodies()).toHaveLength(1));
    expect(putBodies()[0]).toMatchObject({ cpuLimit: "2", applyMode: "now" });
  });

  it("saves with applyMode next_restart when the box is ticked", async () => {
    renderDetail(project());
    fireEvent.click(screen.getByText("Edit Resources"));
    fireEvent.change(screen.getByLabelText("CPU Limit:"), { target: { value: "2" } });
    fireEvent.click(screen.getByLabelText("Apply on next restart"));
    expect(screen.getByText(/new values apply on the next restart/)).toBeInTheDocument();
    fireEvent.click(screen.getByText("Save Changes"));
    await waitFor(() => expect(putBodies()).toHaveLength(1));
    expect(putBodies()[0]).toMatchObject({ cpuLimit: "2", applyMode: "next_restart" });
  });
});

// cluster-services-1382: per-project sandbox image auto-follow switch.
describe("AdminProjectDetail image auto-follow switch", () => {
  beforeEach(() => {
    authFetch.mockReset();
  });

  it("shows the switch on when imageAutoFollow is unset or true", () => {
    renderDetail(project());
    const sw = screen.getByRole("switch", { name: "Auto-follow recommended image" });
    expect(sw).toBeChecked();
  });

  it("shows the switch off and the pinned tag when imageAutoFollow is false", () => {
    renderDetail(project({ imageAutoFollow: false }));
    expect(screen.getByRole("switch", { name: "Auto-follow recommended image" })).not.toBeChecked();
    expect(screen.getByText(/Pinned to v1.0.0/)).toBeInTheDocument();
  });

  it("turning it off sends a settings-only PUT with imageAutoFollow and nothing that rolls the pod", async () => {
    renderDetail(project());
    fireEvent.click(screen.getByRole("switch", { name: "Auto-follow recommended image" }));
    await waitFor(() => expect(putBodies()).toHaveLength(1));
    expect(putBodies()[0]).toEqual({ imageAutoFollow: false });
    for (const k of ["imageTag", "cpuRequest", "cpuLimit", "memoryRequest", "memoryLimit", "storageSize"]) {
      expect(putBodies()[0]).not.toHaveProperty(k);
    }
    const restarts = authFetch.mock.calls.filter(([url]) => String(url).match(/\/(restart|upgrade)$/));
    expect(restarts).toHaveLength(0);
  });
});

describe("AdminProjectDetail secrets", () => {
  beforeEach(() => {
    authFetch.mockReset();
  });

  function renderWithSecrets(secretsResponse: { keys: string[]; externallyManaged: boolean }) {
    const p = project();
    authFetch.mockImplementation((url: string, init?: RequestInit) => {
      if (url.endsWith("/status")) {
        return okJSON({ project: p, deployment: { exists: true, replicas: 1, readyReplicas: 1, availableReplicas: 1, pods: [] } });
      }
      if (url.endsWith("/secrets") && (!init || !init.method || init.method === "GET")) {
        return okJSON(secretsResponse);
      }
      return okJSON({});
    });
    render(<AdminProjectDetail project={p} onClose={() => {}} onRefresh={() => {}} onDelete={() => {}} onRestart={() => {}} />);
    fireEvent.click(screen.getByRole("button", { name: "Manage Secrets" }));
  }

  it("sends null for untouched keys so stored values are kept", async () => {
    renderWithSecrets({ keys: ["ANTHROPIC_BASE_URL", "OPENAI_API_KEY"], externallyManaged: false });

    const openai = await screen.findByLabelText("Value for OPENAI_API_KEY");
    expect(openai).toHaveProperty("value", "");
    expect(openai).toHaveProperty("placeholder", "Unchanged");
    fireEvent.change(openai, { target: { value: "sk-new" } });
    fireEvent.click(screen.getByRole("button", { name: "Save Secrets" }));

    await waitFor(() => expect(putBodies()).toHaveLength(1));
    expect(putBodies()[0]).toEqual({ secrets: { ANTHROPIC_BASE_URL: null, OPENAI_API_KEY: "sk-new" } });
  });

  it("shows ExternalSecret-managed secrets read-only with no save", async () => {
    renderWithSecrets({ keys: ["OPENAI_API_KEY"], externallyManaged: true });

    expect(await screen.findByText(/managed by an ExternalSecret/)).toBeTruthy();
    expect(screen.getByText("OPENAI_API_KEY")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Save Secrets" })).toBeNull();
    expect(screen.queryByText("Add New Secret")).toBeNull();
    expect(screen.queryByLabelText("Value for OPENAI_API_KEY")).toBeNull();
  });
});

describe("AdminProjectDetail secrets keep button", () => {
  beforeEach(() => {
    authFetch.mockReset();
  });

  it("offers Keep stored only for keys that already have a stored value", async () => {
    const p = project();
    authFetch.mockImplementation((url: string) =>
      url.endsWith("/secrets")
        ? okJSON({ keys: ["OPENAI_API_KEY"], externallyManaged: false })
        : okJSON({ project: p, deployment: { exists: true, replicas: 1, readyReplicas: 1, availableReplicas: 1, pods: [] } })
    );
    render(<AdminProjectDetail project={p} onClose={() => {}} onRefresh={() => {}} onDelete={() => {}} onRestart={() => {}} />);
    fireEvent.click(screen.getByRole("button", { name: "Manage Secrets" }));

    fireEvent.change(await screen.findByLabelText("Value for OPENAI_API_KEY"), { target: { value: "sk-new" } });
    fireEvent.change(screen.getByLabelText("Key:"), { target: { value: "NEW_KEY" } });
    fireEvent.change(screen.getByLabelText("Value:"), { target: { value: "v" } });
    fireEvent.click(screen.getByRole("button", { name: "Add Secret" }));

    expect(await screen.findByLabelText("Value for NEW_KEY")).toBeTruthy();
    expect(screen.getAllByRole("button", { name: "Keep stored" })).toHaveLength(1);
    fireEvent.click(screen.getByRole("button", { name: "Keep stored" }));
    expect(screen.getByLabelText("Value for OPENAI_API_KEY")).toHaveProperty("placeholder", "Unchanged");
  });
});
