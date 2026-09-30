// A plain reading of code, enough to tell its parts apart by eye: what
// is a comment, a text, a number, a word of the language. It knows
// families of languages, not languages, and it does not understand what
// it reads: a program that has to be right about code needs a real one.

export type Token = { kind: "plain" | "comment" | "string" | "number" | "keyword"; text: string };

type Family = { line?: string[]; block?: [string, string]; quotes: string[]; words: string[] };

const c: Family = {
  line: ["//"],
  block: ["/*", "*/"],
  quotes: ['"', "'", "`"],
  words:
    "async await break case catch class const continue default defer do else export extends false finally for from func function go if import in interface let new nil null of package range return struct switch this throw true try type typeof undefined var void while yield".split(
      " ",
    ),
};

const hash: Family = {
  line: ["#"],
  quotes: ['"', "'"],
  words:
    "and as case class def do done elif else esac except export false fi finally for from function if import in is lambda None not null or pass raise return then true True False try while with yield".split(
      " ",
    ),
};

const sql: Family = {
  line: ["--"],
  block: ["/*", "*/"],
  quotes: ["'", '"'],
  words:
    "select from where and or not insert into values update set delete create table alter drop index join left right inner outer on group by order having limit as null is in primary key default".split(
      " ",
    ),
};

const markup: Family = { block: ["<!--", "-->"], quotes: ['"', "'"], words: [] };
const none: Family = { quotes: [], words: [] };

const families: Record<string, Family> = {
  js: c, jsx: c, ts: c, tsx: c, go: c, java: c, c, css: c, scss: c, json: c, rs: c,
  py: hash, sh: hash, bash: hash, yml: hash, yaml: hash, toml: hash, rb: hash, dockerfile: hash, env: hash,
  // The names a writer types in a fence, beside the extensions above.
  shell: hash, console: hash, zsh: hash, "shell-session": hash, python: hash, ruby: hash,
  hcl: hash, tf: hash, ini: hash, conf: hash,
  javascript: c, typescript: c, golang: c, rust: c,
  sql,
  html: markup, xml: markup, md: markup,
  txt: none,
};

// The family of a file, by what comes after the last dot of its name.
export function languageOf(name: string): string {
  const base = name.toLowerCase().split("/").pop() ?? "";
  if (base === "dockerfile") return "dockerfile";
  return base.includes(".") ? (base.split(".").pop() as string) : "txt";
}

export function tokens(code: string, language = "txt"): Token[] {
  const family = families[language.toLowerCase()] ?? none;
  const insensitive = family === sql;
  const out: Token[] = [];
  let plain = "";
  const push = (kind: Token["kind"], text: string) => {
    if (plain) out.push({ kind: "plain", text: plain });
    plain = "";
    out.push({ kind, text });
  };

  let i = 0;
  while (i < code.length) {
    const rest = code.slice(i);
    const line = family.line?.find((mark) => rest.startsWith(mark));
    // A `#` or a `//` in the middle of a word is not a comment: a colour,
    // the address of a page.
    if (line && (i === 0 || /[\s;{}()]/.test(code[i - 1]))) {
      const end = code.indexOf("\n", i);
      const text = end < 0 ? rest : code.slice(i, end);
      push("comment", text);
      i += text.length;
      continue;
    }
    if (family.block && rest.startsWith(family.block[0])) {
      const end = code.indexOf(family.block[1], i + family.block[0].length);
      const text = end < 0 ? rest : code.slice(i, end + family.block[1].length);
      push("comment", text);
      i += text.length;
      continue;
    }
    if (family.quotes.includes(code[i])) {
      const quote = code[i];
      let j = i + 1;
      while (j < code.length && code[j] !== quote) {
        if (code[j] === "\\") j++;
        // Only a backtick goes over the end of a line.
        if (code[j] === "\n" && quote !== "`") break;
        j++;
      }
      const text = code.slice(i, Math.min(code.length, j + 1));
      push("string", text);
      i += text.length;
      continue;
    }
    const word = /^[A-Za-z_$][\w$]*/.exec(rest);
    if (word) {
      const known = family.words.includes(insensitive ? word[0].toLowerCase() : word[0]);
      if (known) push("keyword", word[0]);
      else plain += word[0];
      i += word[0].length;
      continue;
    }
    const number = /^\d[\d_]*(\.\d+)?/.exec(rest);
    if (number && (i === 0 || !/[\w$]/.test(code[i - 1]))) {
      push("number", number[0]);
      i += number[0].length;
      continue;
    }
    plain += code[i];
    i++;
  }
  if (plain) out.push({ kind: "plain", text: plain });
  return out;
}

// The same tokens, a list for each line of the code.
export function lines(code: string, language = "txt"): Token[][] {
  const out: Token[][] = [[]];
  for (const token of tokens(code, language)) {
    token.text.split("\n").forEach((text, i) => {
      if (i > 0) out.push([]);
      if (text) out[out.length - 1].push({ kind: token.kind, text });
    });
  }
  return out;
}
