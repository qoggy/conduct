# Writing a Good Node promptTemplate

conduct splits a workflow into a DAG (directed acyclic graph) made of nodes and edges, then gives each node to an AI engine for execution. After rendering, each node's `promptTemplate` is sent to the engine as that node's input. This document explains how to write it well.

## 1. What a Node Can See (Determines What You Can Reference)

Nodes run in **mutually isolated** independent sessions—a node cannot see other nodes' conversation history or tool-call process. It can see only:

1. Its rendered `promptTemplate` (see section 2 for variables)
2. Content it reads from the working directory on disk with tools (such as file-reading tools)

The key point: `{{<node-id>}}` contains the referenced node's **final output text** (the closing artifact returned after the engine finishes), not its full conversation history.

Each agent node executes **at most once** in a run (including continuation after an interruption with `run resume`). To express refinement such as "write → review → revise," split each step into an independent node and connect them with edges: `write(a) → review-and-revise(b)`. Let `b` read `{{a}}` and produce the improved version in one pass.

## 2. Template Variables

Rendering is **plain string replacement** with no escaping. The following variables are supported:

- `{{sys.userPrompt}}` → user request (always injected at runtime)
- `{{sys.cwd}}` → working directory (always injected at runtime)
- `{{sys.runId}}` → run id of the current run (always injected at runtime)
- `{{<node-id>}}` → artifact from the node with that id

The `<node-id>` in `{{<node-id>}}` must identify an **upstream ancestor agent node of this node** (a predecessor reachable along edges). This is enforced during storage validation: references to nonexistent nodes, non-ancestor nodes, or marker nodes `{{START}}` / `{{END}}` (which have no artifacts) are all rejected and cannot be stored. Therefore, in a valid definition, every ancestor referenced by a node's template has already produced output successfully when that node runs (the scheduler guarantees that a node becomes ready only after all of its ancestors have completed).

**Artifacts from parallel branches are not merged automatically**—multiple nodes fanning out from `START` produce output independently and cannot see one another's artifacts. To merge them, have a downstream node reference each branch's `{{id}}` individually in its `promptTemplate` (for example, let `c` read both `{{a}}` and `{{b}}`).

### Output a Literal `{{...}}` → Escape It as `\{{...}}`

If a prompt truly needs the AI to output the literal `{{xxx}}` (for example, when instructing it to write a Jinja / Handlebars template or generate configuration containing double braces), write `\{{xxx}}`. During rendering, the leading backslash is removed and the text is emitted unchanged instead of being treated as a variable. This applies to both known and unknown keys.

## 3. Avoiding Disk-Write Conflicts Between Parallel Branches

conduct schedules nodes as soon as they are ready: when `START` fans out to multiple nodes at the same time, or when dependencies become satisfied for multiple successors of a node, those nodes start concurrently. Concurrent nodes share the same `cwd` for the run. If two parallel nodes both read and write the same working tree on disk (modify the same files or use the same build-output directory), their write order is nondeterministic and they may overwrite or conflict with one another. Working-tree isolation must be designed into the workflow / prompt. There are two approaches:

1. **Design parallel branches as nonconflicting tasks**: for example, make the nodes fanning out from `START` read-only reviews (each writes a review from a different perspective without modifying code), or split independent tasks cleanly along file / directory boundaries so they never touch each other's files (such as separate "frontend" and "backend" directories).
2. **Tell the AI in each branch's prompt to create an independent workspace**: explicitly instruct the node to run `git worktree add` first, creating a directory used only by that node, and then do all work there. For example:

   ```
   Before making changes, run:
   git worktree add ../wt-<branch-key> -b <branch-name>
   Make all subsequent changes in ../wt-<branch-key>; do not touch the current working directory.
   ```

   The downstream merge node then reads each branch's artifact (its diff / change summary) and merges it back into the main working tree.

Scheduling as soon as ready is fixed behavior; users control the disk-write conflict surface of parallel branches through prompts.

## 4. Best Practices

### promptTemplate Is Written for the AI, Not for People

After rendering, it is **sent in full to the LLM**—every word consumes tokens and context and disperses attention. Therefore, put only instructions for the AI in `promptTemplate`. Do not include human-facing notes, design tradeoffs, or explanations of "why this is written this way."

### Wrap Dynamic Content in XML Tags

Content inserted by `{{sys.userPrompt}}` and `{{<node-id>}}` is almost always markdown. Its `## Heading` will mix at the same level as the `## Heading` in your own `promptTemplate`, making it hard for the model to distinguish instructions from data to process. Wrap the content in XML tags (non-markdown boundary markers) so the model can clearly see that content inside the tags is data and content outside is instruction:

```
<user_prompt>
{{sys.userPrompt}}
</user_prompt>

<plan>
{{plan}}
</plan>
```

Choose tag names that match the data's meaning.

**Exception**: if a variable inserts **short, structured output** (such as a score from 0–100, a single word, or a status marker), it contains no markdown headings and will not be confused with instructions, so the XML wrapper may be omitted (though including it is also fine).

### Do Not Insert the Same Variable Repeatedly

Variables are replaced literally, so each occurrence creates another copy. The real cost of repeated insertion is not token expense but **consuming limited context and diluting the model's attention**—the larger the inserted content, the more obvious the effect. By default, each dynamic variable should **appear only once**. If downstream instructions need it again, let the AI refer back to the preceding XML section. Inserting it twice should be a deliberate, considered decision.

Likewise, repeating **short, structured output** such as a score downstream has negligible cost, so this rule need not be applied rigidly there.
