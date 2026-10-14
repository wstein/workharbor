# SPDX-License-Identifier: EUPL-1.2

defmodule WorkHarbor.HostFileFault do
  def open(path, flags) do
    result =
      fail(if(:directory in flags, do: :parent_open, else: :file_open), fn ->
        :file.open(path, flags)
      end)

    case result do
      {:ok, fd} ->
        Process.put({__MODULE__, fd}, if(:directory in flags, do: :parent, else: :file))

      _ ->
        :ok
    end

    result
  end

  def write(fd, bytes) do
    if Process.get({__MODULE__, :stage}) == :write do
      :ok = :file.write(fd, binary_part(bytes, 0, div(byte_size(bytes), 2)))
      {:error, :eio}
    else
      :file.write(fd, bytes)
    end
  end

  def sync(fd), do: fail({Process.get({__MODULE__, fd}), :sync}, fn -> :file.sync(fd) end)

  def close(fd) do
    kind = Process.delete({__MODULE__, fd})
    result = :file.close(fd)
    fail({kind, :close}, fn -> result end)
  end

  def make_link(a, b), do: fail(:link, fn -> :file.make_link(a, b) end)

  defp fail(stage, run) do
    if Process.get({__MODULE__, :stage}) == stage, do: {:error, :eio}, else: run.()
  end
end
