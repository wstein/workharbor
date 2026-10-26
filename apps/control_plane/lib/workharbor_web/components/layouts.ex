# SPDX-License-Identifier: EUPL-1.2

defmodule WorkHarborWeb.Layouts do
  use WorkHarborWeb, :html
  embed_templates "layouts/*"

  attr :flash, :map, required: true
  attr :inner_content, :any, required: true
  attr :current_page, :string, default: nil

  def app(assigns) do
    assigns = assign_new(assigns, :current_page, fn -> nil end)

    ~H"""
    <a class="skip-link" href="#main-content">Skip to content</a>
    <header class="site-header">
      <a class="wordmark" href="/owner">WorkHarbor</a>
      <nav aria-label="Owner navigation">
        <a href="/owner" aria-current={if @current_page == "/owner", do: "page"}>Overview</a>
        <a href="/owner/hosts" aria-current={if @current_page == "/owner/hosts", do: "page"}>Hosts</a>
        <a href="/owner/auth">Access settings</a>
      </nav>
      <.theme_toggle />
    </header>
    <main id="main-content" class="page-content">
      {@inner_content}
    </main>
    <.flash_group flash={@flash} />
    """
  end

  attr :flash, :map, required: true
  attr :id, :string, default: "flash-group"

  def flash_group(assigns) do
    ~H"""
    <div id={@id} aria-live="polite">
      <.flash kind={:info} flash={@flash} />
      <.flash kind={:error} flash={@flash} />
      <.flash
        id="client-error"
        kind={:error}
        title="Connection lost"
        phx-disconnected={JS.remove_attribute("hidden", to: ".phx-client-error #client-error")}
        phx-connected={JS.set_attribute({"hidden", ""})}
        hidden
      >
        Displayed information may be stale. Reconnecting…
      </.flash>
      <.flash
        id="server-error"
        kind={:error}
        title="Page unavailable"
        phx-disconnected={JS.remove_attribute("hidden", to: ".phx-server-error #server-error")}
        phx-connected={JS.set_attribute({"hidden", ""})}
        hidden
      >
        The page could not refresh. Reconnecting…
      </.flash>
    </div>
    """
  end

  def theme_toggle(assigns) do
    ~H"""
    <fieldset class="theme-picker">
      <legend>Color theme</legend>
      <button
        :for={{label, value} <- [{"System", "system"}, {"Light", "light"}, {"Dark", "dark"}]}
        type="button"
        aria-label={label}
        aria-pressed="false"
        data-phx-theme={value}
        phx-click={JS.dispatch("phx:set-theme")}
      >
        {label}
      </button>
    </fieldset>
    """
  end
end
