# SPDX-License-Identifier: EUPL-1.2

defmodule WorkHarbor.FoundationRequestTest do
  use ExUnit.Case, async: true
  alias WorkHarbor.Foundation.{Canonical, Request, RepositoryIdentity}
  alias WorkHarbor.FoundationDatabase, as: DB
  @owner "00000000-0000-4000-8000-000000000001"
  @host "00000000-0000-4000-8000-000000000002"
  @workspace "00000000-0000-4000-8000-000000000003"

  test "native canonical bytes match independent frozen anchors" do
    anchors = [
      {0, "30", "5feceb66ffc86f38d952786c6d696c79c2dbc239dd4e91b46729d73a27fb57e9"},
      {%{"\uE000" => 1, "\u{10000}" => 2}, "7b22ee8080223a312c22f0908080223a327d",
       "8706b5798a29d65739b3d1bbb1d009f87d005c71c8130c5353c4990c15ddfec8"},
      {"\u2028\u2029", "22e280a8e280a922",
       "e3a0e2262ac7790f4df36b522a8fbdd4ee3370d568eb15dc25c5f1775f14ed6c"},
      {"<>&/", "223c3e262f22",
       "396adbc953af00557241e2c859a735e8a11b03279d475359cf45dffed8746492"},
      {%{"b" => true, "i" => 1}, "7b2262223a747275652c2269223a317d",
       "1dc08b312fcdfc60ec48be27868b868ea4846bcb42e831b21f5db31162f31c63"}
    ]

    for {native, hex, digest} <- anchors do
      assert {:ok, bytes} = Canonical.bytes(native)
      assert Base.encode16(bytes, case: :lower) == hex
      assert Base.encode16(:crypto.hash(:sha256, bytes), case: :lower) == digest
    end

    assert {:ok, ~s("\\u001f\\b\\f\\n\\r\\t\\\\\\\"")} =
             Canonical.bytes(<<31, 8, 12, 10, 13, 9, 92, 34>>)

    refute Canonical.bytes("é") == Canonical.bytes("e\u0301")
  end

  test "complete normalized envelope matches independent bytes and semantic field changes" do
    request = DB.request(@host, @workspace)
    {:ok, original} = Request.normalize(@owner, "key", request)

    assert original.bytes ==
             ~s({"action":"create","owner_id":"00000000-0000-4000-8000-000000000001","parameters":{"config":{"commit":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","repository_identity":"git@example.test:private-repository.git","submodules":[]},"host_id":"00000000-0000-4000-8000-000000000002"},"schema_version":1,"workspace_id":"00000000-0000-4000-8000-000000000003"})

    assert original.hash == "cf080204edf198973786c3e5e503dfcc5af4e45caa0909e2a93784fbfa83224f"

    variants = [
      put_in(request, ["parameters", "host_id"], @owner),
      put_in(
        request,
        ["parameters", "config", "repository_identity"],
        "git@EXAMPLE.test:private-repository.git"
      ),
      put_in(request, ["parameters", "config", "commit"], String.duplicate("b", 40)),
      put_in(request, ["parameters", "config", "submodules"], [
        %{"path" => "project", "commit" => String.duplicate("a", 40)}
      ]),
      %{request | "action" => "rebuild", "parameters" => %{"config" => DB.config()}},
      DB.request(@host, @workspace, "start"),
      DB.request(@host, @workspace, "stop"),
      DB.request(@host, @workspace, "remove_runtime")
    ]

    for changed <- variants do
      assert {:ok, normalized} = Request.normalize(@owner, "key", changed)
      refute normalized.hash == original.hash
    end

    {:ok, foreign} = Request.normalize(@host, "key", request)
    refute foreign.hash == original.hash
  end

  test "native canonical domain enforces exact types depth and integer limits" do
    for invalid <- [1.0, :atom, {1}, %{atom: 1}, <<255>>, %{"bad" => <<237, 160, 128>>}] do
      assert {:error, :invalid_json} = Canonical.bytes(invalid)
    end

    depth128 = Enum.reduce(1..128, nil, fn _, nested -> [nested] end)
    assert {:ok, _} = Canonical.bytes(depth128)
    assert {:error, :invalid_json} = Canonical.bytes([depth128])
    assert {:ok, bytes} = Canonical.bytes(Integer.pow(10, 4299))
    assert byte_size(bytes) == 4300
    assert {:error, :invalid_json} = Canonical.bytes(Integer.pow(10, 4300))
    assert {:error, :invalid_json} = Canonical.bytes(-Integer.pow(10, 4300))
    assert {:ok, bytes} = Canonical.bytes(String.duplicate("x", 1_048_577))
    assert byte_size(bytes) == 1_048_579
  end

  test "normalization defaults and submodule ordering preserve semantic identity" do
    request = DB.request(@host, @workspace)
    bare = update_in(request, ["parameters", "config"], &Map.delete(&1, "submodules"))
    assert {:ok, first} = Request.normalize(@owner, "same", request)
    assert {:ok, second} = Request.normalize(@owner, "different-key", bare)
    assert first.bytes == second.bytes
    assert first.hash == second.hash
    assert first.request["owner_id"] == @owner

    entries = [
      %{"path" => "z", "commit" => String.duplicate("b", 40)},
      %{"path" => "a", "commit" => String.duplicate("c", 64)}
    ]

    left = put_in(request, ["parameters", "config", "submodules"], entries)
    right = put_in(request, ["parameters", "config", "submodules"], Enum.reverse(entries))
    assert {:ok, a} = Request.normalize(@owner, "key", left)
    assert {:ok, b} = Request.normalize(@owner, "key", right)
    assert a.bytes == b.bytes

    for action <- ["start", "stop", "remove_runtime"] do
      explicit = DB.request(@host, @workspace, action)

      assert Request.normalize(@owner, "key", explicit) ==
               Request.normalize(@owner, "key", Map.delete(explicit, "parameters"))
    end
  end

  test "native request rejects unknown fields actions nulls and invalid scopes" do
    request = DB.request(@host, @workspace)

    bad = [
      Map.delete(request, "schema_version"),
      %{request | "schema_version" => 1.0},
      Map.put(request, "owner_id", @owner),
      Map.put(request, "timeout", 1),
      %{request | "action" => "delete_data"},
      %{request | "action" => "unknown"},
      %{request | "parameters" => nil},
      %{request | "workspace_id" => String.upcase(@workspace <> "a")},
      put_in(request, ["parameters", "host_id"], "foreign"),
      put_in(request, ["parameters", "config", "commit"], "main"),
      put_in(request, ["parameters", "config", "submodules"], nil),
      put_in(request, ["parameters", "config", "secret"], "canary"),
      Map.put(DB.request(@host, @workspace, "stop"), "parameters", %{"delete_data" => true})
    ]

    for input <- bad,
        do: assert({:error, :invalid_request} = Request.normalize(@owner, "key", input))

    for key <- ["", "white space", "é", String.duplicate("x", 129), nil],
        do: assert({:error, :invalid_request} = Request.normalize(@owner, key, request))

    assert {:ok, _} = Request.normalize(@owner, String.duplicate("x", 128), request)
    assert {:error, :invalid_request} = Request.normalize("wrong-owner", "key", request)

    assert {:error, :invalid_request} =
             Request.normalize(@owner, "key", ~s({"duplicate":1,"duplicate":2}))

    duplicate = List.duplicate(%{"path" => "a", "commit" => String.duplicate("a", 40)}, 2)

    assert {:error, :invalid_request} =
             Request.normalize(
               @owner,
               "key",
               put_in(request, ["parameters", "config", "submodules"], duplicate)
             )

    for path <- [".", "..", "a/../b", "/a", "a\\b", "a/.git/b", "a//b", "a\n"] do
      entries = [%{"path" => path, "commit" => String.duplicate("a", 40)}]

      assert {:error, :invalid_request} =
               Request.normalize(
                 @owner,
                 "key",
                 put_in(request, ["parameters", "config", "submodules"], entries)
               )
    end
  end

  test "native improper submodule lists return a fixed rejection" do
    request = DB.request(@host, @workspace)
    entry = %{"path" => "a", "commit" => String.duplicate("a", 40)}

    for entries <- [[entry | :bad_tail], [:bad | :tail]] do
      assert {:error, :invalid_request} =
               Request.normalize(
                 @owner,
                 "key",
                 put_in(request, ["parameters", "config", "submodules"], entries)
               )
    end

    assert {:error, :invalid_json} = Canonical.bytes([1 | :bad_tail])
  end

  test "lexical repository identities preserve HTTPS SSH and scp without fetch claims" do
    positives = [
      "HTTPS://Host:0443/Repo.git",
      "ssh://git@Host:022/Repo.git",
      "git@Host:Repo.git",
      "git@[::1]:Repo.git",
      "ssh://[2001:db8::1]/repo",
      "https://host./",
      "https://999.999/path",
      "host:/absolute/path:part",
      "host:path@name",
      "https://host/%2fRepo"
    ]

    negatives = [
      "",
      "https://user:password@host/repo",
      "https://user@host/repo",
      "ssh://git:password@host/repo",
      "https://host",
      "ssh://host",
      "git://host/repo",
      "https://under_score/repo",
      "https://a..b/repo",
      "ssh://[:::]/repo",
      "ssh://[fe80::1%eth0]/repo",
      "ssh://host:0/repo",
      "ssh://host:65536/repo",
      "ssh://host:/repo",
      "git@host:",
      "host:path?secret",
      "https://host/r#x",
      "https://host/%xx",
      "https://host/r\\x",
      "https://host/a b",
      "https://host/\u0085",
      "https://host/\u3000",
      <<255>>
    ]

    for value <- positives, do: assert(RepositoryIdentity.valid?(value), inspect(value))
    for value <- negatives, do: refute(RepositoryIdentity.valid?(value), inspect(value))

    hashes =
      for origin <- [
            "https://HOST/repo",
            "https://host/repo",
            "https://host/repo.git",
            "https://host:443/repo"
          ] do
        request =
          put_in(
            DB.request(@host, @workspace),
            ["parameters", "config", "repository_identity"],
            origin
          )

        {:ok, normalized} = Request.normalize(@owner, "key", request)
        normalized.hash
      end

    assert Enum.uniq(hashes) == hashes
  end
end
