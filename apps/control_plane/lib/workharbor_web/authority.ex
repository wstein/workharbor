# SPDX-License-Identifier: EUPL-1.2

defmodule WorkHarborWeb.Authority do
  @moduledoc "Strict shared TLS authority/origin spelling and comparison."
  def parse(raw) when is_binary(raw) and byte_size(raw) <= 320 do
    case Regex.run(~r/\A(\[[0-9a-fA-F:.]+\]|[a-zA-Z0-9.-]+)(?::([0-9]{1,5}))?\z/, raw,
           capture: :all_but_first
         ) do
      [host] -> effective(host, 443)
      [host, ""] -> effective(host, 443)
      [host, port] -> effective(host, String.to_integer(port))
      _ -> :error
    end
  end

  def parse(_), do: :error

  def effective(host, port) when is_binary(host) and is_integer(port) and port in 1..65535 do
    if byte_size(host) <= 320 and String.valid?(host) and Regex.match?(~r/\A[\x21-\x7e]+\z/, host) do
      normalized(host, port)
    else
      :error
    end
  end

  def effective(_, _), do: :error

  defp normalized(host, port) do
    host = String.downcase(host)

    cond do
      String.starts_with?(host, "[") ->
        if String.ends_with?(host, "]"),
          do: ipv6(String.slice(host, 1, byte_size(host) - 2), port),
          else: :error

      String.contains?(host, ":") ->
        ipv6(host, port)

      byte_size(host) > 253 or host == "" ->
        :error

      Enum.all?(
        String.split(host, "."),
        &Regex.match?(~r/\A[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\z/, &1)
      ) ->
        {:ok, {host, port}}

      true ->
        :error
    end
  end

  def origin(value) when is_binary(value) do
    case Regex.run(~r/\Ahttps:\/\/(.+)\z/i, value, capture: :all_but_first) do
      [authority] -> parse(authority)
      _ -> :error
    end
  end

  def origin(_), do: :error

  defp ipv6(host, port) do
    case :inet.parse_address(String.to_charlist(host)) do
      {:ok, tuple} when tuple_size(tuple) == 8 ->
        {:ok, {tuple |> :inet.ntoa() |> to_string(), port}}

      _ ->
        :error
    end
  end
end
