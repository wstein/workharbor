# SPDX-License-Identifier: EUPL-1.2

defmodule WorkHarbor.SpecKitTest do
  use ExUnit.Case, async: false
  import Ecto.Query
  alias WorkHarbor.{Accounts, ConfigTrust, Repo, SpecKit}
  alias WorkHarbor.Foundation.{Host, Owner, Workspace}
  alias WorkHarbor.FoundationDatabase, as: DB

  setup tags do
    Repo.put_dynamic_repo(Repo)
    WorkHarbor.DataCase.setup_sandbox(tags)
    {owner, secret} = WorkHarbor.AuthDatabase.fixture!()
    {:ok, token, principal} = Accounts.login(owner, secret, "cli_owner")
    {:ok, ^principal} = Accounts.authenticate(token)
    host = Repo.insert!(%Host{owner_id: owner})
    %{principal: principal, workspace: workspace(owner, host.id), secret: secret}
  end

  test "workspace_only_request and request_overrides_refused", c do
    assert {:ok, c.workspace.id} == SpecKit.decode_request(%{"workspace_id" => c.workspace.id})

    for key <-
          ~w(path url repository repository_identity commit owner owner_id source_id feature artifact) do
      request = %{"workspace_id" => c.workspace.id, key => "SOURCE-CANARY"}
      assert {:error, :invalid_request} = SpecKit.decode_request(request)
      assert {:error, :invalid_request} = SpecKit.decode_query(URI.encode_query(request))
    end
  end

  test "invalid_request_shape rejects duplicate keys and malformed IDs", c do
    id = c.workspace.id

    assert {:error, :invalid_request} =
             SpecKit.decode_request(~s({"workspace_id":"#{id}","workspace_id":"#{id}"}))

    assert {:error, :invalid_request} =
             SpecKit.decode_query("workspace_id=#{id}&workspace_id=#{id}")

    for invalid <- [nil, %{}, %{"workspace_id" => nil}, %{"workspace_id" => "main"}, []] do
      assert {:error, :invalid_request} = SpecKit.decode_request(invalid)
    end

    assert {:error, :invalid_request} = SpecKit.project(c.principal, "main")
  end

  test "stored_source_and_approval keeps artifacts unavailable without host transport", c do
    assert {:ok, %{availability: :unavailable, reason: :trust_required}} =
             SpecKit.project(c.principal, c.workspace.id)

    assert {:ok, approval} = ConfigTrust.approve(c.principal, DB.config())
    assert {:ok, result} = SpecKit.project(c.principal, c.workspace.id)
    assert result.availability == :unavailable
    assert result.reason == :host_read_unavailable
    assert result.workspace_id == c.workspace.id
    assert result.repository_identity == DB.config()["repository_identity"]
    assert result.commit == DB.config()["commit"]
    assert result.approval_id == approval.id
    refute Map.has_key?(result, :artifacts)
    assert is_integer(result.observed_at)
    assert Repo.aggregate(WorkHarbor.ConfigTrust.Approval, :count) == 1

    Repo.update_all(from(w in Workspace, where: w.id == ^c.workspace.id),
      set: [requested_config: DB.config(String.duplicate("b", 40))]
    )

    assert {:ok, %{reason: :trust_required}} = SpecKit.project(c.principal, c.workspace.id)
  end

  test "foreign_workspace_not_found before source reads for two authenticated owners", c do
    # The product has one primary authenticator. Seed a retained second-owner
    # credential, then obtain its principal through the real authentication path.
    other = Repo.insert!(%Owner{display_name: "Other owner"}).id
    token = "whr1_" <> Base.url_encode64(:crypto.strong_rand_bytes(32), padding: false)

    Repo.insert!(%Accounts.Credential{
      owner_id: other,
      token_hash: :crypto.hash(:sha256, "WorkHarbor credential v1\0" <> token),
      kind: "cli_owner",
      issued_at: Accounts.now(),
      expires_at: Accounts.now() + 60
    })

    assert {:ok, other_principal} = Accounts.authenticate(token)
    host = Repo.insert!(%Host{owner_id: other})
    foreign = workspace(other, host.id)
    {:ok, _} = ConfigTrust.approve(c.principal, DB.config())

    queries =
      capture_queries(fn ->
        assert {:error, :not_found} = SpecKit.project(c.principal, foreign.id)
        assert {:error, :not_found} = SpecKit.project(other_principal, c.workspace.id)
        assert {:error, :not_found} = SpecKit.project(c.principal, Ecto.UUID.generate())
      end)

    assert queries != []
    refute Enum.any?(queries, &String.contains?(&1, "requested_config"))
    refute Enum.any?(queries, &String.contains?(&1, "revision_trust_approvals"))

    assert Repo.query!(
             "SELECT count(*) FROM audit_events WHERE event='authorization_denied' AND action='artifact_projection_read'",
             [],
             log: false
           ).rows == [[3]]
  end

  test "artifact read admits three credential kinds and rejects revoked and forged principals",
       c do
    for kind <- ["cli_owner", "owner_browser", "dashboard"] do
      {:ok, _, principal} = Accounts.login(c.principal.owner_id, c.secret, kind)
      assert {:ok, %{reason: :trust_required}} = SpecKit.project(principal, c.workspace.id)
    end

    assert {:error, :forbidden} =
             SpecKit.project(%{c.principal | owner_id: Ecto.UUID.generate()}, c.workspace.id)

    assert :ok = Accounts.revoke(c.principal, c.principal.credential_id)
    assert {:error, :forbidden} = SpecKit.project(c.principal, c.workspace.id)
  end

  test "bounded source read times out without a result or persistent writes", c do
    ref = make_ref()

    :ok =
      :telemetry.attach(
        ref,
        Repo.config()[:telemetry_prefix] ++ [:query],
        fn _, _, metadata, _ ->
          if String.contains?(metadata.query, "requested_config"), do: Process.sleep(1500)
        end,
        nil
      )

    on_exit(fn -> :telemetry.detach(ref) end)
    started = System.monotonic_time(:millisecond)
    assert {:error, :unavailable} = SpecKit.project(c.principal, c.workspace.id)
    assert System.monotonic_time(:millisecond) - started < 1400
    assert Repo.aggregate(WorkHarbor.ConfigTrust.Approval, :count) == 0
  end

  test "expired artifact credentials cannot read source or approvals", c do
    previous = Application.get_env(:workharbor, :clock)

    on_exit(fn ->
      if previous,
        do: Application.put_env(:workharbor, :clock, previous),
        else: Application.delete_env(:workharbor, :clock)
    end)

    Application.put_env(:workharbor, :clock, fn -> c.principal.expires_at end)

    queries =
      capture_queries(fn ->
        assert {:error, :forbidden} = SpecKit.project(c.principal, c.workspace.id)
      end)

    assert queries != []
    refute Enum.any?(queries, &String.contains?(&1, "requested_config"))
    refute Enum.any?(queries, &String.contains?(&1, "revision_trust_approvals"))
  end

  defp workspace(owner, host),
    do:
      Repo.insert!(%Workspace{
        owner_id: owner,
        host_id: host,
        desired_state: "stopped",
        requested_config: DB.config()
      })

  defp capture_queries(fun) do
    ref = make_ref()
    caller = self()

    :ok =
      :telemetry.attach(
        ref,
        Repo.config()[:telemetry_prefix] ++ [:query],
        fn _, _, metadata, _ -> send(caller, {ref, metadata.query}) end,
        nil
      )

    try do
      fun.()
      drain(ref, [])
    after
      :telemetry.detach(ref)
    end
  end

  defp drain(ref, queries) do
    receive do
      {^ref, query} -> drain(ref, [query | queries])
    after
      0 -> queries
    end
  end
end
