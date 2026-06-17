import { describe, it, expect } from "vitest";
import { scopeEditCanSave, scopePrefillFromSelection, scopeHasInput } from "./scope-helpers";
import type { RawMessage } from "./hooks/use-raw-messages";

// SRS 007 FR-04 / FR-05 — pure UI gating logic (no DOM; @testing-library/react is
// not installed in this repo, matching the voice-picker.test.tsx convention).

describe("scopeEditCanSave", () => {
  const base = {
    hasCallback: true,
    currentAgent: "agent-a",
    currentGraph: "graph-a",
    editAgent: "agent-a",
    editGraph: "graph-a",
    saving: false,
  };

  it("is false when nothing changed", () => {
    expect(scopeEditCanSave(base)).toBe(false);
  });

  it("is false when no save callback is wired", () => {
    expect(scopeEditCanSave({ ...base, hasCallback: false, editGraph: "graph-b" })).toBe(false);
  });

  it("is false while saving", () => {
    expect(scopeEditCanSave({ ...base, editGraph: "graph-b", saving: true })).toBe(false);
  });

  it("is true when only the graph changed (non-empty)", () => {
    expect(scopeEditCanSave({ ...base, editGraph: "graph-b" })).toBe(true);
  });

  it("is true when only the agent changed (graph unchanged, non-empty)", () => {
    expect(scopeEditCanSave({ ...base, editAgent: "agent-b" })).toBe(true);
  });

  it("is true when both changed", () => {
    expect(scopeEditCanSave({ ...base, editAgent: "agent-b", editGraph: "graph-b" })).toBe(true);
  });

  it("is false when graph is cleared to empty (graph is a required scope)", () => {
    expect(scopeEditCanSave({ ...base, editAgent: "agent-b", editGraph: "" })).toBe(false);
  });

  it("is false when graph is whitespace-only (treated as empty)", () => {
    expect(scopeEditCanSave({ ...base, editAgent: "agent-b", editGraph: "   " })).toBe(false);
  });

  it("trims graph before comparing, so a padded-equal graph is not a change", () => {
    // currentGraph "graph-a", editGraph "graph-a  " → trimmed equals current → no change → false
    expect(scopeEditCanSave({ ...base, editGraph: "graph-a  " })).toBe(false);
  });
});

describe("scopePrefillFromSelection", () => {
  const row = (agent_id: string, graph_id: string): Pick<RawMessage, "agent_id" | "graph_id"> => ({
    agent_id,
    graph_id,
  });

  it("returns empty for an empty selection", () => {
    expect(scopePrefillFromSelection([])).toEqual({ agent: "", graph: "" });
  });

  it("prefills the shared agent + graph when homogeneous", () => {
    expect(
      scopePrefillFromSelection([row("a", "g"), row("a", "g"), row("a", "g")]),
    ).toEqual({ agent: "a", graph: "g" });
  });

  it("blanks agent when agents differ, keeps graph when graphs match", () => {
    expect(
      scopePrefillFromSelection([row("a", "g"), row("b", "g")]),
    ).toEqual({ agent: "", graph: "g" });
  });

  it("blanks graph when graphs differ, keeps agent when agents match", () => {
    expect(
      scopePrefillFromSelection([row("a", "g1"), row("a", "g2")]),
    ).toEqual({ agent: "a", graph: "" });
  });

  it("blanks both when both differ", () => {
    expect(
      scopePrefillFromSelection([row("a", "g1"), row("b", "g2")]),
    ).toEqual({ agent: "", graph: "" });
  });
});

describe("scopeHasInput", () => {
  it("is false when both empty", () => {
    expect(scopeHasInput("", "")).toBe(false);
  });

  it("is false when both whitespace-only", () => {
    expect(scopeHasInput("  ", "\t")).toBe(false);
  });

  it("is true when only agent set", () => {
    expect(scopeHasInput("agent-a", "")).toBe(true);
  });

  it("is true when only graph set", () => {
    expect(scopeHasInput("", "graph-a")).toBe(true);
  });

  it("is true when both set", () => {
    expect(scopeHasInput("agent-a", "graph-a")).toBe(true);
  });
});
