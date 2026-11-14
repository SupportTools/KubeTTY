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
