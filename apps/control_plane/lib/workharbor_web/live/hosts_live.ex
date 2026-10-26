# SPDX-License-Identifier: EUPL-1.2

defmodule WorkHarborWeb.HostsLive do
  @moduledoc "Bounded read-only owner host metadata; connection freshness is not provider health."
  use Phoenix.LiveView, layout: {WorkHarborWeb.Layouts, :app}, log: false
  import WorkHarborWeb.CoreComponents, only: [display_text: 1]
  alias WorkHarbor.{Accounts, Hosts}
  alias WorkHarborWeb.Security

  @impl true
  def mount(_params, _session, socket),
    do:
      {:ok,
       assign(socket,
         rows: [],
         state: :loading,
         page: 1,
         size: 25,
         has_next: false,
         observed_at: nil
       )}

  @impl true
  def handle_params(_params, uri, socket) do
    case Security.owner_list_params(URI.parse(uri).query || "") do
      {:ok, %{page: page, size: size}} ->
        {:noreply, socket |> assign(page: page, size: size) |> owner_refresh()}

      _ ->
        {:noreply, assign(socket, rows: [], state: :error)}
    end
  end

  @doc false
  def owner_refresh(socket) do
    case Hosts.list_status(
           socket.private.owner_auth.principal,
           socket.assigns.page,
           socket.assigns.size
         ) do
      {:ok, result} -> assign(socket, Map.put(result, :state, :ready))
      {:error, :forbidden} -> stop(socket)
      _ -> assign(socket, rows: [], state: :error, has_next: false, observed_at: Accounts.now())
    end
  end

  defp stop(socket) do
    state = socket.private.owner_auth
    if state.timer, do: Process.cancel_timer(state.timer)

    socket =
      put_in(socket.private[:owner_auth], %{
        state
        | stopped: true,
          principal: nil,
          timer: nil,
          ref: nil
      })

    socket |> owner_stop() |> redirect(to: "/login/owner")
  end

  @doc false
  def owner_stop(socket),
    do: assign(socket, rows: [], state: :error, has_next: false, observed_at: nil)

  @impl true
  def handle_info(_message, socket), do: {:noreply, socket}
  @impl true
  def handle_event(_event, _payload, socket), do: {:noreply, socket}

  @impl true
  def render(assigns) do
    ~H"""
    <section aria-labelledby="hosts-title">
      <p class="eyebrow">Owner hosts</p>
      <h1 id="hosts-title">Hosts</h1>
      <p>Connection freshness is metadata, not proof of provider health.</p>
      <p :if={@state == :loading} role="status">Loading hosts…</p>
      <p :if={@state == :error} role="status">
        Host information is unavailable. No health conclusion can be drawn.
      </p>
      <p :if={@state == :ready and @rows == []} role="status">No hosts recorded on this page.</p>
      <p :if={@observed_at}>Observed at {display_text(to_string(@observed_at))} (Unix seconds).</p>
      <ul :if={@state == :ready}>
        <li :for={host <- @rows}>
          <h2>{display_text(host.id)}</h2>
          <p>
            {case host.connection do
              "connected" -> "Connected"
              "revoked" -> "Revoked"
              _ -> "Unknown"
            end}
          </p>
          <p>
            Last seen: {display_text(
              if(host.last_seen, do: to_string(host.last_seen), else: "Unknown")
            )}
          </p>
          <ul aria-label="Recorded capabilities">
            <li :for={capability <- host.capabilities}>{display_text(capability)}</li>
          </ul>
        </li>
      </ul>
      <nav aria-label="Host pages">
        <a :if={@page > 1} href={"/owner/hosts?page=#{@page - 1}&size=#{@size}"}>Previous page</a>
        <a :if={@has_next and @page < 1000} href={"/owner/hosts?page=#{@page + 1}&size=#{@size}"}>Next page</a>
      </nav>
    </section>
    """
  end
end
