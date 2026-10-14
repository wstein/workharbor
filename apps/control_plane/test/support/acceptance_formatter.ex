# SPDX-License-Identifier: EUPL-1.2

defmodule WorkHarbor.AcceptanceFormatter do
  @moduledoc false
  use GenServer

  def init(_opts) do
    path = System.fetch_env!("WORKHARBOR_EXUNIT_EVENTS")
    {:ok, File.open!(path, [:write, :exclusive, :utf8])}
  end

  def handle_cast({:suite_started, _opts}, io) do
    emit(io, %{event: "suite_started"})
    {:noreply, io}
  end

  def handle_cast({:test_finished, test}, io) do
    status = if is_nil(test.state), do: "passed", else: "not_passed"

    emit(io, %{event: "test_finished", id: "#{inspect(test.module)}/#{test.name}", status: status})

    {:noreply, io}
  end

  def handle_cast({:suite_finished, _times}, io) do
    emit(io, %{event: "suite_finished"})
    {:noreply, io}
  end

  def handle_cast(_event, io), do: {:noreply, io}

  defp emit(io, event), do: IO.write(io, Jason.encode!(event) <> "\n")
end
