import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, resolve } from "node:path";

import { describe, expect, it } from "vitest";

const currentDir = dirname(fileURLToPath(import.meta.url));
const groupsViewSource = readFileSync(
  resolve(currentDir, "../GroupsView.vue"),
  "utf8",
);
const typesSource = readFileSync(
  resolve(currentDir, "../../../types/index.ts"),
  "utf8",
);

describe("group model operation summary contract", () => {
  it("uses the backend target_platform field in the success formatter", () => {
    expect(typesSource).toContain(
      "export interface GroupModelOperationSummary {\n  target_platform: GroupPlatform",
    );
    expect(groupsViewSource).toContain(
      "platform: platformDisplayName(summary.target_platform)",
    );
    expect(groupsViewSource).not.toContain(
      "platform: platformDisplayName(summary.platform)",
    );
  });
});
