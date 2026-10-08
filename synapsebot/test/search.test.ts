import { describe, expect, it } from "vitest";
import type { Chunk } from "../src/corpus.js";
import { SearchIndex, tokenize, tokenizeQuery } from "../src/search.js";

const chunk = (id: string, title: string, text: string): Chunk => ({
  id, path: id, title, anchor: "", paragraphs: [text],
});

const chunks = [
  chunk("safety", "Safety Model", "Commands are classified before they run. Unknown effects fail closed and ask."),
  chunk("study", "Study Design", "Participants complete tasks in a within-subjects design."),
  chunk("stack", "Stack", "Go runtime, Python pipeline, Ollama serving the model."),
];

describe("tokenize", () => {
  it("lowercases, drops stopwords, and stems", () => {
    expect(tokenize("What are the Commands in the Pipeline?")).toEqual(["command", "pipeline"]);
    expect(new Set(tokenize("serves served serving"))).toEqual(new Set(["serv"]));
    expect(tokenize("class process")).toEqual(["class", "process"]);
  });
});

describe("SearchIndex", () => {
  const index = new SearchIndex(chunks);

  it("ranks the section that matches the question first", () => {
    expect(index.search("how do unknown commands fail closed", 3)[0]!.chunk.id).toBe("safety");
    expect(index.search("who are the participants", 3)[0]!.chunk.id).toBe("study");
  });

  it("returns nothing for a question with no matching terms", () => {
    expect(index.search("hello there", 3)).toEqual([]);
    expect(index.search("   ", 3)).toEqual([]);
  });

  it("uses context at lower weight so follow-ups keep their topic", () => {
    const hits = index.search("why", 3, "what does the safety model classify");
    expect(hits[0]!.chunk.id).toBe("safety");
  });

  it("respects the result limit", () => {
    expect(index.search("model design runtime participants", 2)).toHaveLength(2);
  });
});

describe("tokenizeQuery", () => {
  it("folds misspellings of the project name and adds the thesis's own words", () => {
    expect(tokenizeQuery("Is synapsOS its own operating system")).toEqual(["synapseo", "own", "operat", "os", "system"]);
    expect(tokenizeQuery("SynapseOS")).toEqual(["synapseo"]);
    expect(tokenizeQuery("a distro?")).toEqual(["distro", "distribution", "debian"]);
  });
});
