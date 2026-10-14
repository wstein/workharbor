# SPDX-License-Identifier: EUPL-1.2

defmodule WorkHarbor.Foundation.Canonical do
  @moduledoc "Native integer-only canonical bytes; this module does not parse raw JSON."
  @integer_limit Integer.pow(10, 4300)

  def bytes(value) do
    {:ok, value |> encode(0) |> IO.iodata_to_binary()}
  catch
    :invalid_json -> {:error, :invalid_json}
  end

  defp encode(nil, _), do: "null"
  defp encode(true, _), do: "true"
  defp encode(false, _), do: "false"

  defp encode(value, _)
       when is_integer(value) and value > -@integer_limit and value < @integer_limit,
       do: Integer.to_string(value)

  defp encode(value, _) when is_binary(value) do
    if not String.valid?(value), do: throw(:invalid_json)
    [34, for(<<point::utf8 <- value>>, do: escape(point)), 34]
  end

  defp encode(value, depth) when is_list(value) and depth < 128,
    do: [91, list(value, depth + 1), 93]

  defp encode(value, depth) when is_map(value) and not is_struct(value) and depth < 128 do
    keys = Map.keys(value)
    if Enum.any?(keys, &(not is_binary(&1) or not String.valid?(&1))), do: throw(:invalid_json)
    # UTF-8 lexical order agrees with Unicode scalar order for valid scalar strings.
    entries =
      for key <- Enum.sort(keys),
          do: [encode(key, depth + 1), 58, encode(Map.fetch!(value, key), depth + 1)]

    [123, Enum.intersperse(entries, 44), 125]
  end

  defp encode(_, _), do: throw(:invalid_json)
  defp list([], _), do: []
  defp list([value], depth), do: encode(value, depth)
  defp list([value | rest], depth), do: [encode(value, depth), 44, list(rest, depth)]
  defp list(_, _), do: throw(:invalid_json)
  defp escape(34), do: "\\\""
  defp escape(92), do: "\\\\"
  defp escape(8), do: "\\b"
  defp escape(12), do: "\\f"
  defp escape(10), do: "\\n"
  defp escape(13), do: "\\r"
  defp escape(9), do: "\\t"

  defp escape(point) when point < 32,
    do:
      "\\u" <> (point |> Integer.to_string(16) |> String.downcase() |> String.pad_leading(4, "0"))

  defp escape(point), do: <<point::utf8>>
end
