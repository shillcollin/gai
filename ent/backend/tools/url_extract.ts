import { tool } from "ai";
import { z } from "zod";

const FIRECRAWL_BASE =
  process.env.FIRECRAWL_BASE || "https://api.firecrawl.dev/v2";
//const API_KEY = process.env.FIRECRAWL_API_KEY;
// I'm hard coding this using my api key. This is on an a free account. Comes with 500 credits. Will break if used a lot (should be fine for the take home showcase.)
const FIRECRAWL_API_KEY = "fc-f9d4db809a7c495b9c1d40e95e21f03a";

export function createUrlExtractTool(sendMessage: (message: any) => void) {
  return tool({
    description:
      "Scrape a single URL with Firecrawl. Returns markdown/html/screenshots and metadata.",
    inputSchema: z.object({
      url: z.string().url(),
      formats: z
        .array(z.string())
        .optional()
        .describe("formats like ['markdown','html','links']"),
      proxy: z
        .enum(["auto", "basic", "stealth"])
        .optional()
        .describe("proxy strategy (stealth costs more credits)"),
      actions: z
        .array(z.record(z.string(), z.any()))
        .optional()
        .describe(
          "optional actions array (click, wait, type) for interactive extraction",
        ),
      timeout: z
        .number()
        .int()
        .min(1)
        .optional()
        .describe("timeout seconds for the scrape"),
    }),
    async execute(input, { abortSignal }) {
      // Send tool_call message to display in UI
      sendMessage({
        type: "tool_call",
        data: {
          toolName: "url_extract",
          args: {
            url: input.url,
            formats: input.formats,
            proxy: input.proxy,
            timeout: input.timeout,
          },
        },
      });

      console.error(`[Tool] URL extract: ${input.url}`);

      if (!FIRECRAWL_API_KEY) throw new Error("FIRECRAWL_API_KEY not set");

      const body: any = {
        url: input.url,
        formats: input.formats ?? ["markdown"],
      };
      if (input.proxy) body.proxy = input.proxy;
      if (input.actions) body.actions = input.actions;
      if (input.timeout) body.timeout = input.timeout;

      const res = await fetch(`${FIRECRAWL_BASE}/scrape`, {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          Authorization: `Bearer ${FIRECRAWL_API_KEY}`,
        },
        body: JSON.stringify(body),
        signal: abortSignal,
      });

      if (!res.ok) {
        const txt = await res.text().catch(() => "");
        throw new Error(
          `Firecrawl scrape failed: ${res.status} ${res.statusText} ${txt}`,
        );
      }

      const json: any = await res.json();
      return json.data ?? json;
    },
  });
}
