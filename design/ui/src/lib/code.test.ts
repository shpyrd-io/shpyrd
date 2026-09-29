import { describe, expect, it } from "vitest";
import { languageOf, lines, tokens } from "./code";

const kinds = (code: string, language: string) =>
  tokens(code, language)
    .filter((t) => t.kind !== "plain")
    .map((t) => `${t.kind}:${t.text}`);

describe("tokens", () => {
  it("tells a comment, a text, a number and a word of the language apart", () => {
    expect(kinds('const port = 3000; // the port\nlog("up");', "js")).toEqual([
      "keyword:const",
      "number:3000",
      "comment:// the port",
      'string:"up"',
    ]);
  });

  it("does not take the address of a page for a comment", () => {
    expect(kinds('const url = "https://a.b/c#d";', "js")).toEqual([
      "keyword:const",
      'string:"https://a.b/c#d"',
    ]);
  });

  it("reads a hash as a comment in a shell, and not inside a text", () => {
    expect(kinds('echo "#not" # yes', "sh")).toEqual(['string:"#not"', "comment:# yes"]);
  });

  it("lets a text between backticks go over the end of a line", () => {
    expect(kinds("const a = `one\ntwo`;", "ts")).toEqual(["keyword:const", "string:`one\ntwo`"]);
  });

  it("knows the words of SQL whatever their case", () => {
    expect(kinds("SELECT id FROM apps", "sql")).toEqual(["keyword:SELECT", "keyword:FROM"]);
  });

  it("colours nothing of a language it does not know", () => {
    expect(kinds('const a = "b" // c', "klingon")).toEqual([]);
  });

  it("loses nothing of the code", () => {
    const code = 'a = 1 # b\n"c" + `d`\n';
    expect(tokens(code, "py").map((t) => t.text).join("")).toBe(code);
  });
});

describe("lines", () => {
  it("gives a list for each line, and an empty one for an empty line", () => {
    const read = lines("a\n\nb", "txt");
    expect(read).toHaveLength(3);
    expect(read[1]).toEqual([]);
  });

  it("breaks a comment of many lines into its lines", () => {
    const read = lines("/* one\ntwo */", "js");
    expect(read.map((l) => l[0].kind)).toEqual(["comment", "comment"]);
  });
});

describe("languageOf", () => {
  it("reads the language from what comes after the last dot of the name", () => {
    expect(languageOf("src/server.test.ts")).toBe("ts");
    expect(languageOf("Dockerfile")).toBe("dockerfile");
    expect(languageOf("README")).toBe("txt");
  });
});
