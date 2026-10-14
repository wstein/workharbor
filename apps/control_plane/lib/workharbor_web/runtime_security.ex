# SPDX-License-Identifier: EUPL-1.2

defmodule WorkHarborWeb.RuntimeSecurity do
  @moduledoc "Fail closed before an authenticated listener is started."
  import Bitwise

  @demo [
    "9kwSZZ3zqgPLCQHHHC7Q9KYXXSVttc/mmuUT4ZmFiV1BGre/cOXzP7jNrsMeH4N4",
    "4XBsLMaWd4lPmLPEtg7KgIs8803lJErmCoNVLqsywrCNiiRCgTgMU4U5WdOI+TAl"
  ]
  def validate!(config, owner_http) do
    if config[:server] == true do
      key = config[:secret_key_base]
      tls = config[:https]

      valid =
        is_binary(key) and byte_size(key) >= 64 and key not in @demo and
          config[:debug_errors] == false and config[:code_reloader] == false and
          config[:http] in [nil, false] and config[:force_ssl] in [nil, false] and is_list(tls) and
          is_tuple(tls[:ip]) and is_integer(tls[:port]) and tls[:port] in 1..65535 and
          regular?(tls[:certfile]) and private?(tls[:keyfile]) and
          valid_origin?(owner_http, tls[:port])

      if not valid, do: raise(ArgumentError, "unsafe authentication server configuration")
    end

    :ok
  rescue
    _ -> raise ArgumentError, "unsafe authentication server configuration"
  end

  defp regular?(p), do: is_binary(p) and Path.type(p) == :absolute and File.regular?(p)

  defp private?(p) do
    with true <- regular?(p), {:ok, s} <- File.stat(p), do: (s.mode &&& 0o077) == 0
  end

  defp valid_origin?(owner_http, listener_port) do
    with {:ok, {_, port} = authority} <- WorkHarborWeb.Authority.origin(owner_http[:origin]),
         true <- port == listener_port,
         authorities when is_list(authorities) <- owner_http[:authorities],
         true <-
           Enum.all?(authorities, fn
             {host, p} -> match?({:ok, _}, WorkHarborWeb.Authority.effective(host, p))
             _ -> false
           end) do
      Enum.any?(authorities, fn {host, p} ->
        WorkHarborWeb.Authority.effective(host, p) == {:ok, authority}
      end)
    else
      _ -> false
    end
  end
end
