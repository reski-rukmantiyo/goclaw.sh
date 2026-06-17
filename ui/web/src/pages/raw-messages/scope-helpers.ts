/**
 * Pure helpers for the raw-message scope (agent_id + graph_id) edit UI.
 *
 * Extracted so the gating/prefill logic is unit-testable without DOM rendering
 * (@testing-library/react is not installed in this repo — see
 * voice-picker.test.tsx). The detail dialog and the selection toolbar consume
 * these. See SRS 007 FR-04 / FR-05.
 */

/** A raw-message-like row with the two scope fields the UI edits. */
export interface ScopeScoped {
  agent_id: string;
  graph_id: string;
}

/**
 * Whether the detail-dialog "Save" is enabled for a scope edit.
 * Enabled only when a save callback exists, the edit is not in-flight, at least
 * one field actually changed, and the graph_id is non-empty (graph cannot be
 * cleared — it is a required scope).
 */
export function scopeEditCanSave(opts: {
  hasCallback: boolean;
  currentAgent: string;
  currentGraph: string;
  editAgent: string;
  editGraph: string;
  saving: boolean;
}): boolean {
  const { hasCallback, currentAgent, currentGraph, editAgent, editGraph, saving } = opts;
  if (!hasCallback || saving) return false;
  const graphTrim = editGraph.trim();
  const agentChanged = editAgent !== currentAgent;
  const graphChanged = graphTrim !== currentGraph;
  return (agentChanged || graphChanged) && graphTrim !== "";
}

/**
 * Compute the agent/graph prefill for the batch "Update Scope" dialog from the
 * selected rows: when the selection is homogeneous on a field, prefill that
 * shared value; otherwise (mixed or empty) leave it blank. Blank means
 * "leave unchanged".
 */
export function scopePrefillFromSelection<T extends ScopeScoped>(messages: T[]): {
  agent: string;
  graph: string;
} {
  if (messages.length === 0) return { agent: "", graph: "" };
  const first = messages[0];
  if (!first) return { agent: "", graph: "" };
  const firstAgent = first.agent_id;
  const firstGraph = first.graph_id;
  const sameAgent = messages.every((m) => m.agent_id === firstAgent);
  const sameGraph = messages.every((m) => m.graph_id === firstGraph);
  return { agent: sameAgent ? firstAgent : "", graph: sameGraph ? firstGraph : "" };
}

/** Whether at least one of agent/graph has non-whitespace input (the batch
 * confirm + the server both require ≥1 scope field). */
export function scopeHasInput(agent: string, graph: string): boolean {
  return agent.trim() !== "" || graph.trim() !== "";
}
