/* eslint-disable @next/next/no-img-element */
"use client"

import { useState } from "react"
import clsx from "clsx"

interface ComposerProps {
  disabled: boolean
  allowImage: boolean
  mode: "text" | "json"
  onModeChange: (mode: "text" | "json") => void
  onSubmit: (input: { text: string; image?: { dataUrl: string; mime: string } }) => void
}

const Composer: React.FC<ComposerProps> = ({
  disabled,
  allowImage,
  mode,
  onModeChange,
  onSubmit,
}) => {
  const [text, setText] = useState("")
  const [image, setImage] = useState<{ dataUrl: string; mime: string } | null>(null)
  const [error, setError] = useState<string | null>(null)

  const handleSubmit = (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (!text.trim() && !image) {
      return
    }
    onSubmit({ text: text.trim(), image: image ?? undefined })
    setText("")
    setImage(null)
    setError(null)
  }

  const handleFileChange = async (event: React.ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0]
    if (!file) {
      setImage(null)
      return
    }
    if (!allowImage) {
      setError("This provider does not support image input.")
      event.target.value = ""
      return
    }
    try {
      const dataUrl = await fileToDataUrl(file)
      setImage({ dataUrl, mime: file.type || "image/png" })
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to read image")
    }
  }

  return (
    <form className="composer" onSubmit={handleSubmit}>
      <div className="composer-controls">
        <div className="mode-toggle" role="radiogroup">
          <button
            type="button"
            role="radio"
            aria-checked={mode === "text"}
            className={clsx("pill", { active: mode === "text" })}
            onClick={() => onModeChange("text")}
            disabled={disabled}
          >
            Conversational
          </button>
          <button
            type="button"
            role="radio"
            aria-checked={mode === "json"}
            className={clsx("pill", { active: mode === "json" })}
            onClick={() => onModeChange("json")}
            disabled={disabled}
          >
            Structured JSON
          </button>
        </div>
      </div>

      <textarea
        className="composer-input"
        placeholder="Ask anything, attach an image, or request structured JSON output."
        value={text}
        onChange={(event) => setText(event.target.value)}
        disabled={disabled}
        onKeyDown={(event) => {
          if (event.key === "Enter" && (event.metaKey || event.ctrlKey)) {
            event.currentTarget.form?.requestSubmit()
          }
        }}
      />

      <div className="composer-footer">
        <div className="composer-actions">
          <label className={clsx("ghost", { disabled: disabled || !allowImage })}>
            <input type="file" accept="image/*" disabled={disabled || !allowImage} onChange={handleFileChange} hidden />
            Attach image
          </label>
          {image && (
            <div className="composer-attachment">
              <img src={image.dataUrl} alt="Attachment preview" />
              <button type="button" onClick={() => setImage(null)} className="ghost">
                Remove
              </button>
            </div>
          )}
        </div>
        <div className="composer-submit">
          {error && <span className="composer-error">{error}</span>}
          <button type="submit" className="primary" disabled={disabled}>
            Send
          </button>
        </div>
      </div>
    </form>
  )
}

async function fileToDataUrl(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => {
      if (typeof reader.result === "string") {
        resolve(reader.result)
      } else {
        reject(new Error("Failed to read file"))
      }
    }
    reader.onerror = () => reject(reader.error || new Error("Failed to read file"))
    reader.readAsDataURL(file)
  })
}

export default Composer
