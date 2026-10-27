# SPDX-License-Identifier: EUPL-1.2

defmodule WorkHarborWeb.SpecKitLive do
  @moduledoc "Read-only artifact admission; host reads remain a protocol dependency."
  use Phoenix.LiveView, layout: false, log: false
  import WorkHarborWeb.CoreComponents, only: [display_text: 1]
  alias WorkHarbor.SpecKit

  @impl true
  def mount(_params, _session, socket),
    do: {:ok, assign(socket, workspace_id: nil, result: nil, state: :selection)}

  @impl true
  def handle_params(_params, uri, socket) do
    case URI.parse(uri).query do
      nil ->
        {:noreply, assign(socket, workspace_id: nil, result: nil, state: :selection)}

      raw ->
        case SpecKit.decode_query(raw) do
          {:ok, id} ->
            {:noreply,
             socket
             |> assign(workspace_id: id, state: :loading, result: nil)
             |> dashboard_refresh()}

          _ ->
            {:noreply, assign(socket, workspace_id: nil, result: nil, state: :invalid_request)}
        end
    end
  end

  @doc false
  def dashboard_refresh(%{assigns: %{workspace_id: nil}} = socket), do: socket
  def dashboard_refresh(%{assigns: %{state: :not_found}} = socket), do: socket

  def dashboard_refresh(socket) do
    case SpecKit.project(socket.private.dashboard_auth.principal, socket.assigns.workspace_id) do
      {:ok, result} -> assign(socket, result: result, state: :unavailable)
      {:error, :forbidden} -> stop(socket)
      {:error, :not_found} -> assign(socket, result: nil, state: :not_found)
      _ -> assign(socket, result: nil, state: :unavailable)
    end
  end

  defp stop(socket) do
    state = socket.private.dashboard_auth
    if state.timer, do: Process.cancel_timer(state.timer)

    socket =
      put_in(socket.private[:dashboard_auth], %{
        state
        | stopped: true,
          principal: nil,
          timer: nil,
          ref: nil
      })

    socket |> dashboard_stop() |> redirect(to: "/login/dashboard")
  end

  @doc false
  def dashboard_stop(socket),
    do: assign(socket, result: nil, workspace_id: nil, state: :unavailable)

  @impl true
  def handle_info(_message, socket), do: {:noreply, socket}
  @impl true
  def handle_event(_event, _payload, socket), do: {:noreply, socket}

  @impl true
  def render(assigns) do
    ~H"""
    <a class="skip-link" href="#main-content">Skip to content</a>
    <header class="site-header" data-dashboard>
      <a class="wordmark" href="/dashboard">WorkHarbor</a>
      <nav aria-label="Dashboard navigation">
        <a href="/dashboard/spec-kit" aria-current="page">Spec Kit</a>
      </nav>
    </header>
    <main id="main-content" class="page-content">
      <h1>Spec Kit dashboard</h1>
      <p :if={@state == :selection} role="status">
        No workspace selected.
      </p>
      <p :if={@state == :invalid_request} role="status">Invalid request.</p>
      <p :if={@state == :not_found} role="status">Workspace not found.</p>
      <section :if={@state == :unavailable} aria-labelledby="artifacts-title">
        <h2 id="artifacts-title">Artifacts unavailable</h2>
        <p :if={@result && @result.reason == :trust_required}>
          Existing revision trust is unavailable.
        </p>
        <p :if={@result && @result.reason == :host_read_unavailable}>
          Artifact reads from the workspace host are not yet available.
        </p>
        <dl :if={@result}>
          <dt>Workspace</dt><dd>{display_text(@result.workspace_id)}</dd>
        </dl>
        <dl :if={@result && @result.reason == :host_read_unavailable}>
          <dt>Requested repository identity</dt><dd>{display_text(@result.repository_identity)}</dd>
          <dt>Requested immutable commit</dt><dd>{display_text(@result.commit)}</dd>
          <dt>Retained approval</dt><dd>{display_text(@result.approval_id)}</dd>
        </dl>
        <p :if={@result}>
          Observed at {display_text(to_string(@result.observed_at))} (Unix seconds).
        </p>
        <p>Artifact layout and content provenance are unknown. No artifact bytes have been read.</p>
        <p>Workspace and agent status are unavailable.</p>
      </section>
    </main>
    """
  end
end
