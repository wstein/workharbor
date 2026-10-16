# SPDX-License-Identifier: EUPL-1.2

defmodule WorkHarbor.HostConnections do
  @moduledoc "Serialized socket ownership and conservative liveness; no lifecycle dispatcher."
  use GenServer
  alias WorkHarbor.{Repo, Hosts, Accounts}
  alias WorkHarbor.HostConnections.Protocol
  alias WorkHarbor.Hosts.Peer
  # Contract cadence is 10 s. Each heartbeat costs a transaction and an undeletable audit
  # row, so arrivals are limited by a token bucket (capacity 3, one token per 5 s) measured
  # on receive time, not on the previous commit; the first heartbeat after hello is free.
  @heartbeat_refill_ms 5000
  @heartbeat_capacity 3
  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  def open(peer, socket), do: call(__MODULE__, {:open, peer, socket})
  def receive_hello(ref, message), do: peer_call(ref, {:hello, message})
  def heartbeat(ref, message), do: peer_call(ref, {:heartbeat, message})
  def mark_stale(ref, lease), do: peer_call(ref, {:stale, lease})

  def record_fresh_observation(ref, runtime, generation, intent),
    do: peer_call(ref, {:observe, runtime, generation, intent})

  def disconnect(ref), do: peer_call(ref, :disconnect)

  def project_runtime(principal, runtime) do
    with :ok <- Accounts.authorize(principal, :host_status) do
      case call(__MODULE__, {:project, principal.owner_id, runtime}) do
        {:ok, _} = result -> result
        {:error, :not_found} = result -> result
        _ -> unknown_projection(principal.owner_id, runtime)
      end
    end
  end

  defp runtime_row(owner, runtime),
    do:
      Hosts.one(
        "SELECT r.*,w.owner_id,w.intent_version FROM runtime_instances r JOIN workspaces w ON r.workspace_id=w.id WHERE r.id=? AND w.owner_id=?",
        [runtime, owner]
      )

  defp unknown_projection(owner, runtime) do
    case runtime_row(owner, runtime) do
      nil -> {:error, :not_found}
      row -> {:ok, %{observed_state: "unknown", host_id: row["host_id"]}}
    end
  end

  def host_state(host), do: call(__MODULE__, {:host_state, host})

  defp peer_call({server, ref}, message) when is_pid(server) and is_reference(ref),
    do: call(server, {:peer, ref, message})

  defp peer_call(_, _), do: {:error, :unauthorized_host}

  defp call(server, message) do
    GenServer.call(server, message, 5000)
  catch
    :exit, _ -> {:error, :unauthorized_host}
  end

  @impl true
  def init(opts) do
    Repo.put_dynamic_repo(Keyword.get(opts, :repo, Repo))

    {:ok, _} =
      Hosts.transaction(fn ->
        rows =
          Hosts.sql(
            "SELECT host_id,owner_id FROM host_connection_slots WHERE connection_id IS NOT NULL"
          ).rows

        Hosts.sql("UPDATE host_connection_slots SET state='unknown',connection_id=NULL")

        Enum.each(rows, fn [host, owner] ->
          Hosts.audit(owner, host, "host_connection_reset", "host_restart", "unknown")
        end)
      end)

    Process.send_after(self(), :tick, 1000)
    {:ok, %{peers: %{}, hosts: %{}}}
  end

  @impl true
  def handle_call({:open, %Peer{} = peer, socket}, _, state) when is_pid(socket) do
    if Hosts.current_peer?(peer) do
      id = Ecto.UUID.generate()
      ref = make_ref()
      mono = monotonic()

      case Hosts.transaction(fn ->
             if not Hosts.current_peer?(peer), do: Repo.rollback(:unauthorized_host)

             Hosts.sql(
               "INSERT INTO host_connection_slots(host_id,owner_id,connection_id,state) VALUES(?,?,?,'unknown') ON CONFLICT(host_id) DO UPDATE SET connection_id=excluded.connection_id,state='unknown'",
               [peer.host_id, peer.owner_id, id]
             )

             Hosts.audit(peer.owner_id, peer.host_id, "host_connected", "host_connect", "unknown")
           end) do
        {:ok, _} ->
          # The replacement is committed; only now release the old socket and its topic entry.
          state =
            case state.hosts[peer.host_id] do
              nil -> state
              old -> close(state, old, :replaced)
            end

          Phoenix.PubSub.subscribe(WorkHarbor.PubSub, "host: " <> peer.host_id)
          monitor = Process.monitor(socket)

          expiry =
            mono + max(0, min(peer.expires_at, peer.issuer_expires_at) - Accounts.now()) * 1000

          expiry_timer = Process.send_after(self(), {:expire, ref, id}, max(0, expiry - mono))

          entry = %{
            peer: peer,
            socket: socket,
            monitor: monitor,
            id: id,
            negotiated: false,
            opened: mono,
            seen: nil,
            hb_arrived: nil,
            hb_tokens: 0,
            lease: nil,
            expiry: expiry,
            expiry_timer: expiry_timer
          }

          {:reply, {:ok, {self(), ref}},
           %{
             state
             | peers: Map.put(state.peers, ref, entry),
               hosts: Map.put(state.hosts, peer.host_id, ref)
           }}

        _ ->
          {:reply, {:error, :unauthorized_host}, state}
      end
    else
      {:reply, {:error, :unauthorized_host}, state}
    end
  end

  def handle_call({:open, _, _}, _, state), do: {:reply, {:error, :unauthorized_host}, state}

  def handle_call({:peer, ref, message}, _, state) do
    case state.peers[ref] do
      nil ->
        {:reply, {:error, :unauthorized_host}, state}

      entry ->
        cond do
          not valid_entry?(entry) ->
            {:reply, {:error, :unauthorized_host}, close(state, ref, :identity_invalid)}

          stale?(entry) ->
            {state, result} = close_result(state, ref, :stale)
            reply = if result == :ok, do: {:error, :unauthorized_host}, else: result
            {:reply, reply, state}

          true ->
            peer_message(state, ref, entry, message)
        end
    end
  end

  def handle_call({:project, owner, runtime}, _, state) do
    result =
      Hosts.transaction(fn ->
        case runtime_row(owner, runtime) do
          nil ->
            {:error, :not_found}

          row ->
            entry = state.peers[state.hosts[row["host_id"]]]

            annotation =
              Hosts.one("SELECT * FROM runtime_freshness WHERE runtime_id=?", [runtime])

            known =
              entry && annotation && fresh?(entry) && row["is_current"] == 1 &&
                is_nil(row["deleted_at"]) &&
                annotation["connection_id"] == entry.id &&
                annotation["generation"] === row["generation"] &&
                annotation["intent_version"] === row["intent_version"]

            {:ok,
             %{
               observed_state: if(known, do: row["observed_state"], else: "unknown"),
               host_id: row["host_id"]
             }}
        end
      end)

    reply =
      case result do
        {:ok, value} -> value
        _ -> {:error, :persistence_failed}
      end

    {:reply, reply, state}
  end

  def handle_call({:host_state, host}, _, state) do
    entry = state.peers[state.hosts[host]]
    {:reply, {:ok, if(entry && fresh?(entry), do: "connected", else: "unknown")}, state}
  end

  defp peer_message(state, ref, entry, {:hello, message}) do
    with false <- entry.negotiated,
         {:ok, %{"kind" => "hello"}} <-
           Protocol.validate(message, entry.peer.owner_id, entry.peer.host_id) do
      entry = %{entry | negotiated: true}
      {:reply, {:ok, ack(entry, "hello_ack")}, put_entry(state, ref, entry)}
    else
      _ -> {:reply, {:error, :invalid_connection_message}, close(state, ref, :invalid_message)}
    end
  end

  defp peer_message(state, ref, entry, {:heartbeat, message}) do
    case take_heartbeat_token(entry, monotonic()) do
      {:ok, entry} -> heartbeat(state, ref, entry, message)
      :limited -> {:reply, {:error, :heartbeat_too_fast}, close(state, ref, :heartbeat_too_fast)}
    end
  end

  defp peer_message(state, ref, entry, {:stale, lease}) do
    if entry.lease === lease and stale?(entry) do
      {state, result} = close_result(state, ref, :stale)
      {:reply, result, state}
    else
      {:reply, :ok, state}
    end
  end

  defp peer_message(state, _ref, entry, {:observe, runtime, generation, intent}) do
    if fresh?(entry) and is_integer(generation) and generation > 0 and is_integer(intent) and
         intent > 0 do
      result =
        Hosts.transaction(fn ->
          current_slot!(entry)
          if not fresh?(entry), do: Repo.rollback(:stale_observation)

          row =
            Hosts.one(
              "SELECT r.workspace_id FROM runtime_instances r JOIN workspaces w ON w.id=r.workspace_id WHERE r.id=? AND r.generation=? AND r.host_id=? AND w.owner_id=? AND w.intent_version=? AND r.is_current=1 AND r.deleted_at IS NULL",
              [runtime, generation, entry.peer.host_id, entry.peer.owner_id, intent]
            )

          if is_nil(row), do: Repo.rollback(:stale_observation)

          Hosts.sql(
            "INSERT INTO runtime_freshness(runtime_id,generation,intent_version,connection_id) VALUES(?,?,?,?) ON CONFLICT(runtime_id) DO UPDATE SET generation=excluded.generation,intent_version=excluded.intent_version,connection_id=excluded.connection_id",
            [runtime, generation, intent, entry.id]
          )

          Hosts.audit(
            entry.peer.owner_id,
            entry.peer.host_id,
            "runtime_freshness_recorded",
            "runtime_observed",
            "recorded",
            workspace_id: row["workspace_id"],
            runtime_id: runtime,
            generation: generation,
            intent_version: intent
          )

          :ok
        end)

      {:reply, if(result == {:ok, :ok}, do: :ok, else: result), state}
    else
      {:reply, {:error, :stale_observation}, state}
    end
  end

  defp peer_message(state, ref, _entry, :disconnect) do
    {state, result} = close_result(state, ref, :disconnected)
    {:reply, result, state}
  end

  defp peer_message(state, ref, _entry, _),
    do: {:reply, {:error, :invalid_connection_message}, close(state, ref, :invalid_message)}

  @impl true
  def handle_info(:tick, state) do
    state = sweep(state)
    Process.send_after(self(), :tick, 1000)
    {:noreply, state}
  end

  def handle_info(:identity_changed, state), do: {:noreply, sweep(state)}

  def handle_info({:stale, ref, lease}, state) do
    entry = state.peers[ref]

    {:noreply,
     if(entry && entry.lease === lease && stale?(entry),
       do: close(state, ref, :stale),
       else: state
     )}
  end

  def handle_info({:expire, ref, id}, state) do
    entry = state.peers[ref]

    {:noreply,
     if(entry && entry.id == id && monotonic() >= entry.expiry,
       do: close(state, ref, :identity_invalid),
       else: state
     )}
  end

  def handle_info({:DOWN, monitor, :process, _, _}, state) do
    ref =
      Enum.find_value(state.peers, fn {ref, entry} -> if entry.monitor == monitor, do: ref end)

    {:noreply, if(ref, do: close(state, ref, :disconnected), else: state)}
  end

  defp sweep(state),
    do:
      Enum.reduce(Map.keys(state.peers), state, fn ref, acc ->
        entry = acc.peers[ref]

        cond do
          not valid_entry?(entry) -> close(acc, ref, :identity_invalid)
          stale?(entry) -> close(acc, ref, :stale)
          true -> acc
        end
      end)

  defp close(state, ref, reason), do: elem(close_result(state, ref, reason), 0)

  defp close_result(state, ref, reason) do
    case state.peers[ref] do
      nil ->
        {state, :ok}

      entry ->
        Process.demonitor(entry.monitor, [:flush])
        Process.cancel_timer(entry.expiry_timer)
        Phoenix.PubSub.unsubscribe(WorkHarbor.PubSub, "host: " <> entry.peer.host_id)

        result =
          Hosts.transaction(fn ->
            changed =
              Hosts.sql(
                "UPDATE host_connection_slots SET state='unknown' WHERE host_id=? AND connection_id=?",
                [entry.peer.host_id, entry.id]
              )

            if changed.num_rows == 1,
              do:
                Hosts.audit(
                  entry.peer.owner_id,
                  entry.peer.host_id,
                  "host_connection_unknown",
                  "host_disconnect",
                  "unknown"
                )
          end)

        outcome = if match?({:ok, _}, result), do: :ok, else: {:error, :persistence_failed}

        send(
          entry.socket,
          {:host_connection_close, if(outcome == :ok, do: reason, else: :persistence_failed)}
        )

        {%{
           state
           | peers: Map.delete(state.peers, ref),
             hosts: Map.delete(state.hosts, entry.peer.host_id)
         }, outcome}
    end
  end

  defp current_slot!(entry) do
    row =
      Hosts.one("SELECT host_id FROM host_connection_slots WHERE host_id=? AND connection_id=?", [
        entry.peer.host_id,
        entry.id
      ])

    if is_nil(row), do: Repo.rollback(:unauthorized_host)
  end

  defp valid_entry?(entry),
    do:
      monotonic() < entry.expiry and Hosts.current_peer?(entry.peer) and
        not is_nil(
          Hosts.one(
            "SELECT host_id FROM host_connection_slots WHERE host_id=? AND connection_id=?",
            [entry.peer.host_id, entry.id]
          )
        )

  defp fresh?(entry),
    do: entry.negotiated and not is_nil(entry.seen) and not stale?(entry) and valid_entry?(entry)

  defp heartbeat(state, ref, entry, message) do
    with true <- entry.negotiated,
         {:ok, %{"kind" => "heartbeat"}} <-
           Protocol.validate(message, entry.peer.owner_id, entry.peer.host_id) do
      case Hosts.transaction(fn ->
             current_slot!(entry)
             if not Hosts.current_peer?(entry.peer), do: Repo.rollback(:unauthorized_host)
             now = DateTime.from_unix!(Accounts.now()) |> DateTime.to_iso8601()
             lease = DateTime.from_unix!(Accounts.now() + 30) |> DateTime.to_iso8601()

             Hosts.sql("UPDATE hosts SET last_seen=?,lease_expires_at=? WHERE id=?", [
               now,
               lease,
               entry.peer.host_id
             ])

             Hosts.sql(
               "UPDATE host_connection_slots SET state='connected' WHERE host_id=? AND connection_id=?",
               [entry.peer.host_id, entry.id]
             )

             Hosts.audit(
               entry.peer.owner_id,
               entry.peer.host_id,
               "host_heartbeat",
               "host_heartbeat",
               "authenticated"
             )
           end) do
        {:ok, _} ->
          entry = %{entry | seen: monotonic(), lease: make_ref()}
          Process.send_after(self(), {:stale, ref, entry.lease}, 30000)
          {:reply, {:ok, ack(entry, "heartbeat_ack")}, put_entry(state, ref, entry)}

        _ ->
          {:reply, {:error, :persistence_failed}, close(state, ref, :persistence_failed)}
      end
    else
      _ -> {:reply, {:error, :invalid_connection_message}, close(state, ref, :invalid_message)}
    end
  end

  defp take_heartbeat_token(%{hb_arrived: nil} = entry, now),
    do: {:ok, %{entry | hb_arrived: now, hb_tokens: @heartbeat_capacity * @heartbeat_refill_ms}}

  defp take_heartbeat_token(entry, now) do
    tokens =
      min(
        @heartbeat_capacity * @heartbeat_refill_ms,
        entry.hb_tokens + max(0, now - entry.hb_arrived)
      )

    if tokens >= @heartbeat_refill_ms,
      do: {:ok, %{entry | hb_arrived: now, hb_tokens: tokens - @heartbeat_refill_ms}},
      else: :limited
  end

  defp stale?(entry), do: monotonic() - (entry.seen || entry.opened) >= 30000

  defp monotonic,
    do:
      Application.get_env(:workharbor, :host_monotonic_clock, fn ->
        System.monotonic_time(:millisecond)
      end).()

  defp ack(entry, kind),
    do: %{
      "schema_version" => 1,
      "kind" => kind,
      "owner_id" => entry.peer.owner_id,
      "host_id" => entry.peer.host_id
    }

  defp put_entry(state, ref, entry), do: %{state | peers: Map.put(state.peers, ref, entry)}
end
