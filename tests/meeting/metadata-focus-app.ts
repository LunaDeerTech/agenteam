import { createApp, h, ref } from "vue";
import type { DirectoryAgent } from "../../web-crape/src/api/agent-directory";
import type { MeetingDetail } from "../../web-crape/src/api/meetings";
import MeetingMetadataEditor from "../../web-crape/src/components/meetings/MeetingMetadataEditor.vue";
import "../../web-crape/src/styles/tokens.css";
import "../../web-crape/src/styles/base.css";
import "../../web-crape/src/styles/components.css";

const id = (n: number) =>
  `01990000-0000-7000-8000-${n.toString(16).padStart(12, "0")}`;
const at = "2026-10-14T00:00:00Z";
const agents: DirectoryAgent[] = [6, 5].map((n) => ({
  id: id(n),
  project_id: id(1),
  name: `agent-${n}`,
  display_name: `Agent ${n}`,
  tag_color: null,
  description: "",
  version: "1",
  created_at: at,
  updated_at: at,
}));
const detail: MeetingDetail = {
  session: {
    id: id(2),
    project_id: id(1),
    version: "1",
    status: "active",
    title: "Focus fixture",
    summary: null,
    deleting: false,
    created_at: at,
    updated_at: at,
  },
  participants: agents.map((agent, order) => ({
    id: id(order + 10),
    meeting_id: id(2),
    kind: "agent",
    source_id: agent.id,
    name: agent.display_name!,
    order,
    active: true,
  })),
  references: [],
  turns: [],
  decisions: [],
};

declare global {
  interface Window {
    metadataFocusFixture: {
      setReadMode(mode: "hold" | "idle"): void;
      releaseRead(): void;
      observe(): Record<string, string | boolean | number>;
    };
  }
}

// Only the parent directory-read completion is controlled. The editor, dialog,
// layer manager, styles and browser focus behavior are the production modules.
createApp({
  setup() {
    const busy = ref(false);
    let mode: "hold" | "idle" = "hold";
    window.metadataFocusFixture = {
      setReadMode(value) {
        mode = value;
      },
      releaseRead() {
        busy.value = false;
      },
      observe() {
        const edit = document.querySelector<HTMLButtonElement>(
          ".metadata-actions > button",
        );
        const refs = document.querySelector<HTMLButtonElement>(
          ".metadata-actions > button:nth-child(2)",
        );
        const active = document.activeElement;
        return {
          busy: busy.value,
          edit_disabled: edit?.disabled ?? true,
          edit_connected: edit?.isConnected ?? false,
          app_inert: document.getElementById("app")?.inert ?? false,
          dialog_count: [
            ...document.querySelectorAll('[role="dialog"]'),
          ].filter((node) => !node.closest('[inert], [aria-hidden="true"]'))
            .length,
          active:
            active === document.body
              ? "body"
              : active === edit
                ? "edit"
                : active === refs
                  ? "references"
                  : active?.closest('[role="dialog"]')
                    ? "dialog"
                    : "other",
        };
      },
    };
    return () =>
      h("main", { style: "padding: 24px" }, [
        h("h1", "Metadata focus fixture"),
        h(MeetingMetadataEditor, {
          key: detail.session.id,
          detail,
          agents,
          moreAgents: false,
          agentError: "",
          mutable: !busy.value,
          busy: busy.value,
          unsettled: false,
          canLookup: false,
          feedback: "",
          submit: async () => true,
          lookup: async () => null,
          onAgents: () => {
            if (mode === "hold") busy.value = true;
          },
        }),
        h("button", { id: "other-action" }, "Other page action"),
      ]);
  },
}).mount("#app");
