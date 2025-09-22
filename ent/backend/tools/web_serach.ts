import { tool } from "ai";
import { z } from "zod";

const FIRECRAWL_BASE =
  process.env.FIRECRAWL_BASE || "https://api.firecrawl.dev/v2";
//const API_KEY = process.env.FIRECRAWL_API_KEY;
// I'm hard coding this using my api key. This is on an a free account. Comes with 500 credits. Will break if used a lot (should be fine for the take home showcase.)
const FIRECRAWL_API_KEY = "fc-f9d4db809a7c495b9c1d40e95e21f03a";

export function createWebSearchTool(sendMessage: (message: any) => void) {
  return tool({
    description:
      "Search the web via Firecrawl. Optionally scrape the top results (markdown/html/screenshots).",
    inputSchema: z.object({
      query: z.string().min(1).describe("Search query"),
      limit: z
        .number()
        .int()
        .min(1)
        .max(50)
        .optional()
        .describe("Max number of results (default 3)"),
      scrape: z
        .boolean()
        .optional()
        .describe("If true, fetch full content for each result"),
      formats: z
        .array(z.string())
        .optional()
        .describe("Formats for scraped content, e.g. ['markdown']"),
      sources: z
        .array(z.string())
        .optional()
        .describe("sources: ['web','news','images']"),
      categories: z
        .array(z.string())
        .optional()
        .describe("search categories, e.g. ['github','research']"),
      tbs: z
        .string()
        .optional()
        .describe("time-based search filter, e.g. 'qdr:d'"),
    }),
    async execute(input, { abortSignal }) {
      // Send tool_call message to display in UI
      sendMessage({
        type: "tool_call",
        data: {
          toolName: "web_search",
          args: {
            query: input.query,
            limit: input.limit,
            scrape: input.scrape,
            sources: input.sources,
            categories: input.categories,
          },
        },
      });

      console.error(`[Tool] Web search: ${input.query}`);

      if (!FIRECRAWL_API_KEY) {
        throw new Error("FIRECRAWL_API_KEY not set in environment");
      }

      const body: any = {
        query: input.query,
        limit: input.limit ?? 3,
      };

      if (input.sources) body.sources = input.sources;
      if (input.categories) body.categories = input.categories;
      if (input.tbs) body.tbs = input.tbs;

      if (input.scrape) {
        body.scrapeOptions = {
          formats: input.formats ?? ["markdown"],
        };
      }

      const res = await fetch(`${FIRECRAWL_BASE}/search`, {
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
          `Firecrawl search failed: ${res.status} ${res.statusText} ${txt}`,
        );
      }

      const json: any = await res.json();
      // Firecrawl SDKs usually return { success: true, data: { ... } }
      return json.data ?? json;
    },
  });
}
