# SPDX-License-Identifier: EUPL-1.2

defmodule WorkHarbor.Hosts.PrivateFile do
  @moduledoc false
  import Bitwise

  def traversal(path, missing_final \\ false) when is_binary(path) do
    if Path.type(path) != :absolute or Path.expand(path) != path do
      {:error, :unsafe_identity}
    else
      parts = Path.split(path)

      result =
        Enum.with_index(parts)
        |> Enum.reduce_while("", fn {part, index}, prefix ->
          current = if part == "/", do: "/", else: Path.join(prefix, part)

          case File.lstat(current) do
            {:ok, %{type: :symlink}} ->
              {:halt, {:error, :unsafe_identity}}

            {:ok, _} ->
              {:cont, current}

            {:error, :enoent} when missing_final and index == length(parts) - 1 ->
              {:cont, current}

            _ ->
              {:halt, {:error, :unsafe_identity}}
          end
        end)

      if is_binary(result), do: :ok, else: result
    end
  end

  def directory(path) when is_binary(path) do
    with :ok <- traversal(path),
         {:ok, stat} <- File.lstat(path),
         true <- stat.type == :directory and (stat.mode &&& 0o777) == 0o700 and stat.uid == uid(),
         do: :ok,
         else: (_ -> {:error, :unsafe_identity})
  end

  def directory(_), do: {:error, :unsafe_identity}

  def read(path) do
    with :ok <- traversal(path),
         :ok <- directory(Path.dirname(path)),
         {:ok, stat} <- File.lstat(path),
         true <- stat.type == :regular and (stat.mode &&& 0o777) == 0o600 and stat.uid == uid(),
         true <- stat.size <= 32768,
         {:ok, bytes} <- File.read(path),
         do: {:ok, bytes},
         else: (_ -> {:error, :unsafe_identity})
  end

  def create(path, bytes) do
    with :ok <- traversal(path, true), :ok <- directory(Path.dirname(path)) do
      temporary = path <> "." <> Base.url_encode64(:crypto.strong_rand_bytes(12), padding: false)

      try do
        with {:ok, fd} <-
               io().open(String.to_charlist(temporary), [:write, :binary, :exclusive, :raw]),
             :ok <- File.chmod(temporary, 0o600),
             result <- write_close(fd, bytes),
             :ok <- result,
             :ok <- io().make_link(String.to_charlist(temporary), String.to_charlist(path)),
             :ok <- sync_directory(Path.dirname(path)),
             do: :ok,
             else: (
               {:error, :eexist} -> {:error, :already_initialized}
               _ -> {:error, :persistence_failed}
             )
      after
        File.rm(temporary)
      end
    end
  end

  defp write_close(fd, bytes) do
    result =
      try do
        with :ok <- io().write(fd, bytes), do: io().sync(fd)
      rescue
        _ -> {:error, :eio}
      catch
        _, _ -> {:error, :eio}
      end

    closed = io().close(fd)
    if result == :ok, do: closed, else: result
  end

  defp sync_directory(dir) do
    with {:ok, fd} <- io().open(String.to_charlist(dir), [:read, :raw, :directory]) do
      result =
        try do
          io().sync(fd)
        rescue
          _ -> {:error, :eio}
        catch
          _, _ -> {:error, :eio}
        end

      closed = io().close(fd)
      if result == :ok, do: closed, else: result
    end
  end

  # The process-local fault selector is compiled only into test builds. The
  # production module has only the native :file implementation, no hook/config.
  if Mix.env() == :test do
    defp io, do: Process.get({__MODULE__, :test_io}, :file)
  else
    defp io, do: :file
  end

  # OTP has no public geteuid BIF. Fixed POSIX id has no caller-controlled argv,
  # shell, secret or network; it verifies the effective service account.
  defp uid do
    {value, 0} = System.cmd("/usr/bin/id", ["-u"])
    value |> String.trim() |> String.to_integer()
  end
end
