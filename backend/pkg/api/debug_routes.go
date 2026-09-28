package api

import (
	"github.com/gofiber/fiber/v2"

	"backend/pkg/engine"
	"backend/pkg/llm"
)

// registerDebugRoutes mounts dev-only debugging endpoints. These are
// intentionally unauthenticated-safe: they are stateless and touch no
// user data — they replay a pasted LLM transcript through the stream
// parser and report what WOULD be written to the VFS. Gate with
// DEBUG_EVENTS/GO_ENV when deploying publicly.
func registerDebugRoutes(app *fiber.App, hub *engine.EngineHub) {
	// POST /api/debug/replay  body: {"transcript": "..."}
	app.Post("/api/debug/replay", func(c *fiber.Ctx) error {
		var body struct {
			Transcript string `json:"transcript"`
		}
		if err := c.BodyParser(&body); err != nil || body.Transcript == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "transcript is required"})
		}

		parser := llm.NewStreamParser()
		type fileOut struct {
			Path    string `json:"path"`
			Content string `json:"content"`
			Bytes   int    `json:"bytes"`
		}
		var files []fileOut
		var events []string
		apply := func(evs []llm.StreamEvent) {
			for _, ev := range evs {
				switch ev.Kind {
				case llm.EventStart:
					events = append(events, "file_generating "+ev.Path)
				case llm.EventChunk:
					events = append(events, "file_chunk "+ev.Path)
				case llm.EventEnd:
					events = append(events, "file_generated "+ev.Path)
					files = append(files, fileOut{Path: ev.Path, Content: ev.Content, Bytes: len(ev.Content)})
				}
			}
		}
		// Feed in realistic chunk sizes (not one blob) to exercise the
		// incremental state machine exactly like production streaming.
		const chunk = 64
		t := body.Transcript
		for i := 0; i < len(t); i += chunk {
			e := i + chunk
			if e > len(t) {
				e = len(t)
			}
			apply(parser.Feed(t[i:e]))
		}
		apply(parser.Flush())

		return c.JSON(fiber.Map{
			"files":  files,
			"events": events,
		})
	})
}
