# SPDX-License-Identifier: EUPL-1.2

defmodule WorkHarbor.StrictJSON do
  @moduledoc "Bounded raw JSON to native values, before duplicate keys can collapse."
  @max_bytes 1_048_576
  def decode(raw) when is_binary(raw) and byte_size(raw) > @max_bytes,
    do: {:error, :input_limit_exceeded}

  def decode(raw) when is_binary(raw) do
    with true <- String.valid?(raw) and not String.starts_with?(raw, <<239, 187, 191>>),
         :ok <- preflight(raw, false, false, [], 0),
         {:ok, value} <-
           Jason.decode(raw, objects: :ordered_objects, keys: :strings, strings: :copy) do
      validate(value)
      {:ok, native(value)}
    else
      _ -> {:error, :invalid_json}
    end
  catch
    :invalid_json -> {:error, :invalid_json}
  end

  def decode(_), do: {:error, :invalid_json}

  def decode_object(raw) do
    case decode(raw) do
      {:ok, v} when is_map(v) and not is_struct(v) -> {:ok, v}
      {:ok, _} -> {:error, :invalid_json}
      error -> error
    end
  end

  # This lexical budget pass deliberately leaves full syntax and scalar escapes
  # to locked Jason. It builds neither numbers nor request objects.
  defp preflight(<<>>, false, false, [], _), do: :ok
  defp preflight(<<>>, _, _, _, _), do: {:error, :invalid_json}

  defp preflight(<<_, rest::binary>>, true, true, stack, _),
    do: preflight(rest, true, false, stack, 0)

  defp preflight(<<92, rest::binary>>, true, false, stack, _),
    do: preflight(rest, true, true, stack, 0)

  defp preflight(<<34, rest::binary>>, true, false, stack, _),
    do: preflight(rest, false, false, stack, 0)

  defp preflight(<<_, rest::binary>>, true, false, stack, _),
    do: preflight(rest, true, false, stack, 0)

  defp preflight(<<34, rest::binary>>, false, _, stack, _),
    do: preflight(rest, true, false, stack, 0)

  defp preflight(<<c, rest::binary>>, false, _, stack, _) when c in [123, 91] do
    if length(stack) >= 128,
      do: {:error, :invalid_json},
      else: preflight(rest, false, false, [c | stack], 0)
  end

  defp preflight(<<c, rest::binary>>, false, _, [o | stack], _)
       when (c == 125 and o == 123) or (c == 93 and o == 91),
       do: preflight(rest, false, false, stack, 0)

  defp preflight(<<c, _::binary>>, false, _, _, _) when c in [125, 93, 46],
    do: {:error, :invalid_json}

  defp preflight(<<c, rest::binary>>, false, _, stack, digits) when c in 48..57 do
    if digits >= 4300,
      do: {:error, :invalid_json},
      else: preflight(rest, false, false, stack, digits + 1)
  end

  defp preflight(<<c, _::binary>>, false, _, _, digits) when c in [101, 69] and digits > 0,
    do: {:error, :invalid_json}

  defp preflight(<<_, rest::binary>>, false, _, stack, _),
    do: preflight(rest, false, false, stack, 0)

  defp validate(%Jason.OrderedObject{values: values}) do
    keys = Enum.map(values, &elem(&1, 0))
    if length(keys) != MapSet.size(MapSet.new(keys)), do: throw(:invalid_json)
    Enum.each(values, fn {_, value} -> validate(value) end)
  end

  defp validate(v) when is_list(v), do: Enum.each(v, &validate/1)
  defp validate(v) when is_float(v), do: throw(:invalid_json)
  defp validate(_), do: :ok

  defp native(%Jason.OrderedObject{values: values}),
    do: Map.new(values, fn {k, v} -> {k, native(v)} end)

  defp native(v) when is_list(v), do: Enum.map(v, &native/1)
  defp native(v) when is_float(v), do: throw(:invalid_json)
  defp native(v), do: v
end
