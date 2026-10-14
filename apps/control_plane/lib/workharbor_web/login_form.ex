# SPDX-License-Identifier: EUPL-1.2

defmodule WorkHarborWeb.LoginForm do
  @moduledoc false
  def decode(raw, _) when is_binary(raw) and byte_size(raw) > 4096,
    do: {:error, :input_limit_exceeded}

  def decode(raw, fields) when is_binary(raw) do
    pairs = String.split(raw, "&")

    with true <- length(pairs) <= 3,
         {:ok, values} <- decode_pairs(pairs, fields, []) do
      {:ok, Map.new(values)}
    else
      _ -> {:error, :invalid_form}
    end
  end

  def decode(_, _), do: {:error, :invalid_form}

  defp decode_pairs([], fields, values) do
    if Enum.sort(Enum.map(values, &elem(&1, 0))) == Enum.sort(fields),
      do: {:ok, values},
      else: :error
  end

  defp decode_pairs([pair | rest], fields, values) do
    with [k, v] <- String.split(pair, "=", parts: 2),
         true <- percent?(k) and percent?(v),
         key <- URI.decode_www_form(k),
         value <- URI.decode_www_form(v),
         true <- String.valid?(key) and String.valid?(value) and value != "",
         true <- key in fields and not List.keymember?(values, key, 0) do
      decode_pairs(rest, fields, [{key, value} | values])
    else
      _ -> :error
    end
  end

  defp percent?(<<>>), do: true

  defp percent?(<<37, a, b, rest::binary>>)
       when a in ~c"0123456789abcdefABCDEF" and b in ~c"0123456789abcdefABCDEF",
       do: percent?(rest)

  defp percent?(<<37, _::binary>>), do: false
  defp percent?(<<_, rest::binary>>), do: percent?(rest)
end
