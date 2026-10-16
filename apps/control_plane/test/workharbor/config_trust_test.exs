# SPDX-License-Identifier: EUPL-1.2

defmodule WorkHarbor.ConfigTrustTest do
  use ExUnit.Case, async: false
  alias WorkHarbor.{Accounts, ConfigTrust, Repo}
  alias WorkHarbor.FoundationDatabase, as: DB

  setup do
    path = Path.join(System.tmp_dir!(), "workharbor-trust-#{Ecto.UUID.generate()}.db")
    pid = WorkHarbor.ConfigTrustDatabase.open!(path)
    Application.put_env(:workharbor, :clock, fn -> 2_000_000_000 end)
    {owner, secret} = WorkHarbor.AuthDatabase.fixture!()
    {:ok, _token, principal} = Accounts.login(owner, secret, "cli_owner")

    on_exit(fn ->
      Application.delete_env(:workharbor, :clock)
      if Process.alive?(pid), do: GenServer.stop(pid)
      for suffix <- ["", "-wal", "-shm"], do: File.rm(path <> suffix)
    end)

    %{owner: owner, secret: secret, principal: principal}
  end

  test "denial inside the transaction keeps its audit when authorization changes after the pre-check",
       ctx do
    expire_after_precheck(ctx.principal, :config_approve)
    assert {:error, :forbidden} = ConfigTrust.approve(ctx.principal, DB.config())

    assert WorkHarbor.Repo.query!(
             "SELECT count(*) FROM audit_events WHERE event='authorization_denied' AND action='config_approve'",
             [],
             log: false
           ).rows == [[1]]
  end

  test "exact approval repeats without a second approval or audit", ctx do
    descriptor = DB.config()
    assert {:ok, first} = ConfigTrust.approve(ctx.principal, descriptor)
    assert first.owner_id == ctx.owner
    assert first.descriptor == descriptor
    assert first.approved_at == 2_000_000_000
    assert {:ok, second} = ConfigTrust.approve(ctx.principal, descriptor)
    assert first.id == second.id
    assert {:ok, ^first} = ConfigTrust.lookup(ctx.principal, first.id, descriptor)
    assert DB.count("revision_trust_approvals") == 1

    assert DB.sql("SELECT count(*) FROM audit_events WHERE event='revision_approved'").rows == [
             [1]
           ]
  end

  test "changed config submodule origin and complete set require new approval", ctx do
    d =
      Map.put(DB.config(), "submodules", [
        %{"path" => "projects/app", "commit" => String.duplicate("b", 40)}
      ])

    assert {:ok, approval} = ConfigTrust.approve(ctx.principal, d)

    for changed <- [
          Map.put(d, "commit", String.duplicate("c", 40)),
          Map.put(d, "repository_identity", "git@EXAMPLE.test:private-repository.git"),
          Map.put(d, "submodules", []),
          put_in(d, ["submodules", Access.at(0), "commit"], String.duplicate("d", 40))
        ] do
      assert {:error, :trust_required} = ConfigTrust.lookup(ctx.principal, approval.id, changed)
      assert {:ok, next} = ConfigTrust.approve(ctx.principal, changed)
      refute next.id == approval.id
    end
  end

  test "unknown null malformed descriptors never approve", ctx do
    for bad <- [
          nil,
          %{},
          Map.put(DB.config(), "owner_id", ctx.owner),
          Map.put(DB.config(), "commit", "main"),
          Map.put(DB.config(), "submodules", nil)
        ] do
      assert {:error, :invalid_configuration} = ConfigTrust.approve(ctx.principal, bad)
    end

    assert DB.count("revision_trust_approvals") == 0
  end

  test "dashboard revoked and forged principals cannot approve", ctx do
    {:ok, _token, dashboard} = Accounts.login(ctx.owner, ctx.secret, "dashboard")
    assert {:error, :forbidden} = ConfigTrust.approve(dashboard, DB.config())
    forged = %{ctx.principal | owner_id: Ecto.UUID.generate()}
    assert {:error, :forbidden} = ConfigTrust.approve(forged, DB.config())
    assert :ok = Accounts.revoke(ctx.principal, ctx.principal.credential_id)
    assert {:error, :forbidden} = ConfigTrust.approve(ctx.principal, DB.config())
  end

  test "dashboard may read exact approval but cannot use a foreign owner", ctx do
    assert {:ok, approval} = ConfigTrust.approve(ctx.principal, DB.config())
    {:ok, _token, dashboard} = Accounts.login(ctx.owner, ctx.secret, "dashboard")
    assert {:ok, _} = ConfigTrust.lookup(dashboard, approval.id, DB.config())

    assert {:error, :forbidden} =
             ConfigTrust.lookup(
               %{dashboard | owner_id: Ecto.UUID.generate()},
               approval.id,
               DB.config()
             )
  end

  test "expired credentials cannot approve or read", ctx do
    Application.put_env(:workharbor, :clock, fn -> ctx.principal.expires_at end)
    assert {:error, :forbidden} = ConfigTrust.approve(ctx.principal, DB.config())

    assert {:error, :forbidden} =
             ConfigTrust.lookup(ctx.principal, Ecto.UUID.generate(), DB.config())
  end

  test "approval identity is immutable and owner FK is enforced", ctx do
    assert {:ok, approval} = ConfigTrust.approve(ctx.principal, DB.config())

    assert_raise Exqlite.Error, fn ->
      DB.sql("UPDATE revision_trust_approvals SET approved_at=0 WHERE id=?", [approval.id])
    end

    assert_raise Exqlite.Error, fn ->
      DB.sql("DELETE FROM revision_trust_approvals WHERE id=?", [approval.id])
    end

    assert_raise Exqlite.Error, fn ->
      DB.sql(
        """
        INSERT INTO revision_trust_approvals
        (id,owner_id,descriptor,descriptor_bytes,descriptor_sha256,approved_at,inserted_at,updated_at)
        VALUES (?,?,'{}',X'00','foreign',0,'now','now')
        """,
        [Ecto.UUID.generate(), Ecto.UUID.generate()]
      )
    end

    assert {:ok, ^approval} = ConfigTrust.lookup(ctx.principal, approval.id, DB.config())
    refute inspect(approval) =~ "private-repository"
  end

  test "host approval lookup rechecks genuine current assignment and revocation", ctx do
    dir =
      Path.join(
        WorkHarbor.HostFixture.physical_temp(),
        "whr-trust-issuer-#{Ecto.UUID.generate()}"
      )

    previous = Application.get_env(:workharbor, :host_identity)

    on_exit(fn ->
      if previous,
        do: Application.put_env(:workharbor, :host_identity, previous),
        else: Application.delete_env(:workharbor, :host_identity)

      File.rm_rf!(dir)
    end)

    assert {:ok, _} = WorkHarbor.Hosts.Issuer.initialize(dir)
    Application.put_env(:workharbor, :host_identity, issuer_dir: dir)
    assert {:ok, approval} = ConfigTrust.approve(ctx.principal, DB.config())
    assert {:ok, issued} = WorkHarbor.Hosts.issue_enrollment(ctx.principal, nil)
    {csr, _} = WorkHarbor.HostFixture.csr()
    assert {:ok, enrolled} = WorkHarbor.Hosts.redeem(issued.code, csr)

    assert {:ok, peer} =
             WorkHarbor.Hosts.resolve_identity(Base.decode64!(enrolled["certificate"]), :active)

    assert {:ok, ^approval} = ConfigTrust.lookup_for_host(peer, approval.id, DB.config())

    assert {:error, :trust_required} =
             ConfigTrust.lookup_for_host(peer, approval.id, DB.config(String.duplicate("c", 40)))

    assert {:error, :forbidden} =
             ConfigTrust.lookup_for_host(
               %{peer | owner_id: Ecto.UUID.generate()},
               approval.id,
               DB.config()
             )

    assert :ok = WorkHarbor.Hosts.revoke(ctx.principal, peer.host_id)
    assert {:error, :forbidden} = ConfigTrust.lookup_for_host(peer, approval.id, DB.config())
  end

  test "concurrent independent writers record one immutable revision approval", ctx do
    repo = Repo.get_dynamic_repo()

    results =
      for _ <- 1..4 do
        Task.async(fn ->
          Repo.put_dynamic_repo(repo)
          ConfigTrust.approve(ctx.principal, DB.config())
        end)
      end
      |> Enum.map(&Task.await(&1, 5000))

    assert Enum.all?(results, &match?({:ok, _}, &1))
    assert results |> Enum.map(fn {:ok, row} -> row.id end) |> Enum.uniq() |> length() == 1
    assert DB.count("revision_trust_approvals") == 1
  end

  # Counts clock reads of one pre-check, then expires the credential right after it, so only
  # the in-transaction check can deny.
  defp expire_after_precheck(principal, action) do
    {:ok, counter} = Agent.start_link(fn -> 0 end)
    real = Application.get_env(:workharbor, :clock)
    now = WorkHarbor.Accounts.now()

    Application.put_env(:workharbor, :clock, fn ->
      Agent.update(counter, &(&1 + 1))
      now
    end)

    :ok = WorkHarbor.Accounts.authorize(principal, action)
    reads = Agent.get(counter, & &1)
    Agent.update(counter, fn _ -> 0 end)

    Application.put_env(:workharbor, :clock, fn ->
      if Agent.get_and_update(counter, &{&1 + 1, &1 + 1}) > reads, do: now + 10_000_000, else: now
    end)

    on_exit(fn ->
      if real,
        do: Application.put_env(:workharbor, :clock, real),
        else: Application.delete_env(:workharbor, :clock)
    end)
  end
end
