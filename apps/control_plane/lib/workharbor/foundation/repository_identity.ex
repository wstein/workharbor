# SPDX-License-Identifier: EUPL-1.2

defmodule WorkHarbor.Foundation.RepositoryIdentity do
  @moduledoc "Literal lexical origin identity, without fetching or remote alias equivalence."
  @username ~r/\A[A-Za-z0-9._-]+\z/
  @host ~r/\A[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?(?:\.[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?)*\.?\z/
  @forbidden ~r/[\x00-\x20\x7f\x{0085}\x{00a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\\?#]/u

  def valid?(value) when is_binary(value) do
    String.valid?(value) and value != "" and not Regex.match?(@forbidden, value) and
      not String.contains?(Regex.replace(~r/%[0-9A-Fa-f]{2}/, value, ""), "%") and shape?(value)
  end

  def valid?(_), do: false

  defp shape?(value) do
    case Regex.run(~r/\A(https|ssh):\/\/([^\/]+)(\/.*)\z/is, value) do
      [_, scheme, authority, _path] ->
        authority?(authority, String.downcase(scheme) == "ssh", true)

      _ ->
        not Regex.match?(~r/\A[A-Za-z][A-Za-z0-9+.-]*:\/\//, value) and scp?(value)
    end
  end

  defp scp?(value) do
    case Regex.run(~r/\A((?:[A-Za-z0-9._-]+@)?(?:\[[^\]]+\]|[^:]+)):(.+)\z/s, value) do
      [_, authority, _path] -> authority?(authority, true, false)
      _ -> false
    end
  end

  defp authority?(authority, username_allowed, port_allowed) do
    case String.split(authority, "@") do
      [host] ->
        host_port?(host, port_allowed)

      [username, host] ->
        username_allowed and Regex.match?(@username, username) and host_port?(host, port_allowed)

      _ ->
        false
    end
  end

  defp host_port?(authority, ports) do
    captures =
      if String.starts_with?(authority, "["),
        do: Regex.run(~r/\A(\[[^\]]+\])(?::([0-9]+))?\z/, authority),
        else: Regex.run(~r/\A([^:]+)(?::([0-9]+))?\z/, authority)

    case captures do
      [_, host] -> host?(host)
      [_, host, port] -> ports and host?(host) and port?(port)
      _ -> false
    end
  end

  defp host?("[" <> rest) do
    literal = String.trim_trailing(rest, "]")

    case :inet.parse_address(String.to_charlist(literal)) do
      {:ok, tuple} when tuple_size(tuple) == 8 -> true
      _ -> false
    end
  end

  defp host?(host), do: Regex.match?(@host, host)

  defp port?(port) do
    # No enormous integer conversion is needed to classify a port with leading zeros.
    significant = String.trim_leading(port, "0")
    byte_size(significant) in 1..5 and String.to_integer(significant) <= 65535
  end
end
