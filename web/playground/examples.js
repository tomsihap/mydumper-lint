// Examples of the mistakes mydumper-lint exists for. web/playground's
// examples_test (make playground-test) checks each one against the rules it
// must trigger. Whitespace that matters is written ${"  "}, so that no
// editor strips it.
export const examples = [
  {
    title: "A line of spaces drops the masking",
    expect: ["MDL102"],
    version: "v1.0.5-1",
    content: `[client]
host=db
user=backup

[mydumper]
threads=4
${"  "}
[\`app\`.\`users\`]
\`email\`=random_string
`,
  },
  {
    title: "routines=0 enables routines",
    expect: ["MDL402"],
    version: "v1.0.5-1",
    content: `[mydumper]
routines=0
no-data=false
`,
  },
  {
    title: "A misspelled masking function",
    expect: ["MDL502"],
    version: "v1.0.5-1",
    content: `[mydumper]
threads=4

[\`app\`.\`users\`]
\`email\`=random_strng
\`phone\`=random_int
`,
  },
  {
    title: "threads=010 means 8",
    expect: ["MDL407"],
    version: "v1.0.5-1",
    content: `[mydumper]
threads=010
chunk-filesize=64
`,
  },
  {
    title: "A comment with [brackets], then an empty line",
    expect: ["MDL108"],
    version: "v1.0.5-1",
    content: `[mydumper]
# see [the docs] for the options

threads=4
`,
  },
  {
    title: "An empty line in a Windows (CRLF) file",
    expect: ["MDL103"],
    version: "v0.19.3-3",
    crlf: true,
    content: `[mydumper]
threads=4

compress=zstd
`,
  },
  {
    title: "A per-product group mydumper does not read",
    expect: ["MDL201"],
    version: "v0.19.3-3",
    content: `[mydumper]
threads=4

[mydumper_mysql]
no-data=1
`,
  },
  {
    title: "; is not a comment",
    expect: ["MDL304", "MDL401"],
    version: "v1.0.5-1",
    content: `[mydumper]
; nightly dump
threads=4
`,
  },
];
