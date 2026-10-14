# SPDX-License-Identifier: EUPL-1.2

defmodule WorkHarbor.StrictJSONTest do
  use ExUnit.Case, async: true
  alias WorkHarbor.StrictJSON, as: J

  test "decoded duplicate keys reject at every depth before maps" do
    for raw <- [~S({"a":1,"\u0061":2}), ~S({"x":[{"a":1,"a":2}]}), ~S({"\ud800\udc00":1,"𐀀":2})],
        do: assert({:error, :invalid_json} == J.decode(raw))

    assert {:ok, %{"é" => 1, "é" => 2}} = J.decode(~S({"é":1,"é":2}))
  end

  test "strict Unicode floats syntax and huge numeric forms reject safely" do
    for raw <- [
          <<255>>,
          <<239, 187, 191, 123, 125>>,
          ~S("\ud800"),
          ~S("\udc00"),
          ~S("\udc00\ud800"),
          "1.0",
          "1e0",
          "NaN",
          "Infinity",
          "{}{}",
          "{",
          ~S("\q"),
          "1." <> String.duplicate("1", 100_000),
          "1e" <> String.duplicate("1", 100_000)
        ],
        do: assert({:error, :invalid_json} == J.decode(raw))

    assert {:ok, "𐀀"} = J.decode(~S("\ud800\udc00"))
  end

  test "exact byte budget rejects oversize before invalid UTF8" do
    assert {:ok, %{}} = J.decode("{}" <> String.duplicate(" ", 1_048_574))
    assert {:error, :input_limit_exceeded} = J.decode(<<255>> <> String.duplicate(" ", 1_048_576))
    assert {:error, :invalid_json} = J.decode(nil)
  end

  test "integer and container boundaries retain minus sign semantics" do
    for sign <- ["", "-"] do
      raw = sign <> String.duplicate("1", 4300)
      assert {:ok, n} = J.decode(raw)
      assert Integer.to_string(n) == raw
      assert {:error, :invalid_json} = J.decode(raw <> "1")
    end

    for {o, c} <- [{"[", "]"}, {"{\"x\":", "}"}] do
      assert {:ok, _} = J.decode(String.duplicate(o, 128) <> "0" <> String.duplicate(c, 128))

      assert {:error, :invalid_json} =
               J.decode(String.duplicate(o, 129) <> "0" <> String.duplicate(c, 129))
    end

    assert {:ok, 0} = J.decode("-0")
    assert {:ok, _} = J.decode(~S({"x":"[12345{\\\"}"}))
  end

  test "native values retain frozen scalar ordering and API object shape" do
    assert {:ok, value} = J.decode(~S({"𐀀":2,"":1}))
    assert {:ok, bytes} = WorkHarbor.Foundation.Canonical.bytes(value)
    assert Base.encode16(bytes, case: :lower) == "7b22ee8080223a312c22f0908080223a327d"

    for raw <- ["", "[]", "1", "null"],
        do: assert({:error, :invalid_json} == J.decode_object(raw))
  end

  test "fixed forms reject decoded aliases malformed escapes unknown fields and byte overflow" do
    alias WorkHarborWeb.LoginForm, as: F
    fields = ["owner_id", "owner_secret", "_csrf_token"]

    assert {:ok, %{"owner_id" => "x", "owner_secret" => "a b", "_csrf_token" => "x"}} =
             F.decode("owner_id=x&owner_secret=a+b&_csrf_token=x", fields)

    for raw <- [
          "owner_id=x&%6fwner_id=y",
          "owner_id=%GG",
          "owner_id=%FF",
          "owner_id=x&owner_secret[a]=b",
          "owner_id=x&&owner_secret=y",
          "owner_id=x&x=y",
          "owner_id=x&owner_secret=y&_csrf_token=x&x=y"
        ],
        do: assert({:error, :invalid_form} == F.decode(raw, fields))

    assert {:error, :input_limit_exceeded} = F.decode(String.duplicate("x", 4097), fields)
  end

  test "all seventy four frozen raw byte parse vectors match canonical bytes and rejection" do
    file = Path.expand("../../../../contracts/migration/vectors.json", __DIR__)

    cases =
      File.read!(file)
      |> Jason.decode!()
      |> Map.fetch!("cases")
      |> Enum.filter(&(&1["operation"] == "parse-json" and &1["input"]["kind"] == "bytes"))

    assert length(cases) == 74

    for c <- cases do
      raw = expand(c["input"]["data"])
      expected = c["product_expected"]

      case {J.decode(raw), expected["status"]} do
        {{:ok, value}, "ok"} ->
          assert {:ok, bytes} = WorkHarbor.Foundation.Canonical.bytes(value)
          assert bytes == expand(expected["canonical"]), c["id"]

          assert Base.encode16(:crypto.hash(:sha256, bytes), case: :lower) == expected["digest"],
                 c["id"]

        {{:error, code}, "reject"} ->
          assert String.replace(Atom.to_string(code), "_", "-") == expected["code"], c["id"]

        {result, _} ->
          flunk("vector mismatch #{c["id"]}: #{inspect(result, limit: 1)}")
      end
    end
  end

  defp expand(%{"encoding" => "hex", "hex" => hex} = data) do
    bytes = Base.decode16!(hex, case: :mixed)
    assert byte_size(bytes) == data["length"]
    assert Base.encode16(:crypto.hash(:sha256, bytes), case: :lower) == data["sha256"]
    bytes
  end

  defp expand(%{"encoding" => "repeat"} = data) do
    bytes =
      Base.decode16!(data["prefix_hex"], case: :mixed) <>
        :binary.copy(Base.decode16!(data["unit_hex"], case: :mixed), data["count"]) <>
        Base.decode16!(data["suffix_hex"], case: :mixed)

    assert byte_size(bytes) == data["length"]
    assert Base.encode16(:crypto.hash(:sha256, bytes), case: :lower) == data["sha256"]
    bytes
  end
end
