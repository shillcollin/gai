from playwright.sync_api import sync_playwright, expect
import json
import time

def run_verification():
    with sync_playwright() as p:
        browser = p.chromium.launch(headless=True)
        page = browser.new_page()

        try:
            # A more realistic mocked stream with text deltas and multiple tool calls
            mocked_stream_response = [
                {"type": "text.delta", "text_delta": "Okay, I can help with that. ", "seq": 1, "ts": time.time()},
                {"type": "text.delta", "text_delta": "First, I'll search for the weather in San Francisco. ", "seq": 2, "ts": time.time()},
                {
                    "type": "tool.call",
                    "step": 1,
                    "seq": 3,
                    "ts": time.time(),
                    "tool_call": {
                        "id": "tool_call_sf",
                        "name": "web_search",
                        "input": {"query": "weather in San Francisco"}
                    }
                },
                {"type": "text.delta", "text_delta": "Then, I'll search for the weather in Tokyo.", "seq": 4, "ts": time.time()},
                {
                    "type": "tool.call",
                    "step": 2,
                    "seq": 5,
                    "ts": time.time(),
                    "tool_call": {
                        "id": "tool_call_tokyo",
                        "name": "web_search",
                        "input": {"query": "weather in Tokyo"}
                    }
                },
                {
                    "type": "finish",
                    "seq": 6,
                    "ts": time.time(),
                    "usage": {"input_tokens": 25, "output_tokens": 50, "total_tokens": 75},
                    "finish_reason": {"type": "tool_calls"}
                }
            ]
            mocked_body = "\n".join(json.dumps(event) for event in mocked_stream_response)

            page.route(
                "**/api/chat/stream",
                lambda route: route.fulfill(
                    status=200,
                    content_type="application/x-ndjson",
                    body=mocked_body
                )
            )

            with page.expect_response("**/api/providers"):
                page.goto("http://localhost:3000")

            print("Successfully waited for providers API response.")

            prompt_input = page.get_by_placeholder("Ask anything, attach an image, or request structured JSON output.")
            expect(prompt_input).to_be_visible()
            prompt_input.fill("What's the weather in San Francisco and Tokyo?")

            send_button = page.get_by_role("button", name="Send")
            send_button.click()

            # Verify the streamed text is displayed
            expect(page.get_by_text("Okay, I can help with that.")).to_be_visible()
            expect(page.get_by_text("First, I'll search for the weather in San Francisco.")).to_be_visible()
            expect(page.get_by_text("Then, I'll search for the weather in Tokyo.")).to_be_visible()

            # Verify both tool calls are displayed
            tool_call_1 = page.get_by_text("web_search").first
            tool_call_2 = page.get_by_text("web_search").nth(1)

            expect(tool_call_1).to_be_visible(timeout=10000)
            expect(tool_call_2).to_be_visible(timeout=10000)

            # Take a screenshot to visually confirm the entire UI state
            page.screenshot(path="jules-scratch/verification/verification.png")
            print("Screenshot saved to jules-scratch/verification/verification.png")

        except Exception as e:
            print(f"An error occurred: {e}")
            page.screenshot(path="jules-scratch/verification/error.png")
            print("Error screenshot saved to jules-scratch/verification/error.png")
        finally:
            browser.close()

if __name__ == "__main__":
    run_verification()