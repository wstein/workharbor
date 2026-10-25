import {Socket} from "phoenix"
import {LiveSocket} from "phoenix_live_view"
import {hooks as colocatedHooks} from "phoenix-colocated/workharbor"

const savedTheme = () => {
  try { return localStorage.getItem("phx:theme") } catch { return "system" }
}
const systemTheme = () => matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light"
const setTheme = selected => {
  const choice = ["light", "dark"].includes(selected) ? selected : "system"
  document.documentElement.dataset.theme = choice === "system" ? systemTheme() : choice
  document.documentElement.dataset.themeSource = choice
  document.querySelectorAll("[data-phx-theme]").forEach(button => {
    button.setAttribute("aria-pressed", String(button.dataset.phxTheme === choice))
  })
}
setTheme(savedTheme())
window.addEventListener("phx:set-theme", event => {
  const button = event.target.closest("[data-phx-theme]")
  if (!button) return
  const choice = button.dataset.phxTheme
  try {
    if (choice === "system") localStorage.removeItem("phx:theme")
    else localStorage.setItem("phx:theme", choice)
  } catch { /* System preference still works when storage is unavailable. */ }
  setTheme(choice)
})
window.addEventListener("storage", event => {
  if (event.key === "phx:theme") setTheme(event.newValue)
})
matchMedia("(prefers-color-scheme: dark)").addEventListener("change", () => {
  if (document.documentElement.dataset.themeSource === "system") setTheme("system")
})
window.addEventListener("phx:page-loading-stop", () => setTheme(savedTheme()))
window.addEventListener("phx:page-loading-start", () => {
  document.querySelector("main")?.setAttribute("aria-busy", "true")
})
window.addEventListener("phx:page-loading-stop", () => {
  document.querySelector("main")?.removeAttribute("aria-busy")
})

const csrfToken = document.querySelector("meta[name='csrf-token']")?.getAttribute("content")
if (csrfToken) {
  const liveSocket = new LiveSocket("/owner/live", Socket, {
    params: {_csrf_token: csrfToken},
    hooks: {...colocatedHooks},
  })
  liveSocket.connect()
}
