# SPDX-License-Identifier: EUPL-1.2

defmodule WorkHarborWeb.AgentSocket do
  @behaviour WebSock
  alias WorkHarbor.HostConnections
  alias WorkHarbor.HostConnections.Protocol
  @impl true
  def init(peer) do
    Logger.metadata(workharbor_agent_transport: true)

    case HostConnections.open(peer, self()) do
      {:ok, {worker, _} = ref} ->
        {:ok, %{ref: ref, peer: peer, hello: false, worker_monitor: Process.monitor(worker)}}

      _ ->
        {:stop, :normal, 1008, %{ref: nil, peer: peer}}
    end
  end

  @impl true
  def handle_in({raw, [opcode: :text]}, state) when byte_size(raw) <= 4096 do
    with {:ok, message} <- Protocol.decode(raw, state.peer.owner_id, state.peer.host_id),
         expected = if(state.hello, do: "heartbeat", else: "hello"),
         true <- message["kind"] == expected,
         {:ok, ack} <-
           if(state.hello,
             do: HostConnections.heartbeat(state.ref, message),
             else: HostConnections.receive_hello(state.ref, message)
           ) do
      {:push, {:text, Jason.encode!(ack)}, %{state | hello: true}}
    else
      _ -> {:stop, :normal, 1008, state}
    end
  end

  def handle_in(_, state), do: {:stop, :normal, 1008, state}
  @impl true
  def handle_info({:host_connection_close, _}, state),
    do: {:stop, :normal, 1008, state}

  def handle_info({:DOWN, monitor, :process, _, _}, %{worker_monitor: monitor} = state),
    do: {:stop, :normal, 1008, state}

  def handle_info(_, state), do: {:ok, state}
  @impl true
  def terminate(_, state), do: HostConnections.disconnect(state.ref)
end
