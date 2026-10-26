# SPDX-License-Identifier: EUPL-1.2

defmodule WorkHarborWeb.OverviewLive do
  use Phoenix.LiveView, layout: {WorkHarborWeb.Layouts, :app}, log: false

  @impl true
  def mount(_params, _session, socket), do: {:ok, assign(socket, current_page: "/owner")}

  @impl true
  def handle_params(_params, _uri, socket), do: {:noreply, socket}

  @impl true
  def handle_info(_message, socket), do: {:noreply, socket}

  @impl true
  def handle_event(_event, _payload, socket), do: {:noreply, socket}

  @impl true
  def render(assigns) do
    ~H"""
    <section aria-labelledby="overview-title">
      <p class="eyebrow">Owner overview</p>
      <h1 id="overview-title">Your WorkHarbor</h1>
      <p class="intro">A place to check your hosts and workspaces.</p>
      <div class="overview-grid">
        <article aria-labelledby="hosts-title">
          <h2 id="hosts-title">Hosts</h2>
          <p><a href="/owner/hosts">View hosts</a></p>
          <p>Check recorded metadata and connection freshness.</p>
        </article>
        <article aria-labelledby="workspaces-title">
          <h2 id="workspaces-title">Workspaces</h2>
          <p class="status-label">Unavailable</p>
          <p>Workspace information is not available here yet.</p>
        </article>
        <article aria-labelledby="trust-title">
          <h2 id="trust-title">Enrollment and trust</h2>
          <p class="status-label">Unavailable</p>
          <p>Enrollment and trust information is not available here yet.</p>
        </article>
      </div>
    </section>
    """
  end
end
