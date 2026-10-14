# SPDX-License-Identifier: EUPL-1.2

defmodule WorkHarbor.AccountsTest do
  use ExUnit.Case, async: false
  alias WorkHarbor.{Accounts, Repo}
  alias WorkHarbor.FoundationDatabase, as: DB

  setup do
    path = Path.join(System.tmp_dir!(), "workharbor-auth-#{Ecto.UUID.generate()}.db")
    pid = WorkHarbor.AuthDatabase.open!(path)
    Application.put_env(:workharbor, :clock, fn -> 2_000_000_000 end)

    on_exit(fn ->
      Application.delete_env(:workharbor, :clock)
      if Process.alive?(pid), do: GenServer.stop(pid)
      for s <- ["", "-wal", "-shm"], do: File.rm(path <> s)
    end)

    %{path: path, pid: pid}
  end

  test "bootstrap delivery failure never reinitializes owner" do
    assert {:error, :delivery_failed} = Accounts.bootstrap(fn _ -> raise "SECRET-CANARY" end)
    assert DB.count("owner_authenticators") == 1
    assert DB.count("owners") == 1
    assert {:error, :already_initialized} = Accounts.bootstrap(fn _ -> flunk("redelivery") end)
  end

  test "bootstrap writers serialize a single verifier" do
    repo = Repo.get_dynamic_repo()
    parent = self()

    results =
      for _ <- 1..4 do
        Task.async(fn ->
          Repo.put_dynamic_repo(repo)

          Accounts.bootstrap(fn s ->
            send(parent, {:delivered, s})
            :ok
          end)
        end)
      end
      |> Enum.map(&Task.await(&1, 5000))

    assert Enum.count(results, &match?({:ok, _}, &1)) == 1
    assert Enum.count(results, &(&1 == {:error, :already_initialized})) == 3
    assert_receive {:delivered, s}
    assert byte_size(s) == 43
    refute_receive {:delivered, _}
  end

  test "primary auth fixed scopes and hashes preserve secret safety" do
    {owner, secret} = WorkHarbor.AuthDatabase.fixture!()
    assert {:error, :unauthenticated} = Accounts.login(Ecto.UUID.generate(), secret, "cli_owner")

    assert {:error, :unauthenticated} =
             Accounts.login(owner, String.duplicate("x", 43), "cli_owner")

    for {kind, ttl} <- [
          {"cli_owner", 2_592_000},
          {"owner_browser", 28_800},
          {"dashboard", 28_800}
        ] do
      assert {:ok, token, p} = Accounts.login(owner, secret, kind)
      assert byte_size(token) == 48
      assert p.expires_at - p.issued_at == ttl
      assert {:ok, _} = Accounts.authenticate(token, kind)
      assert {:error, :unauthenticated} = Accounts.authenticate(token, "unknown")
      refute inspect(Repo.all(Accounts.Credential)) =~ token
    end

    refute inspect(DB.sql("SELECT * FROM owner_authenticators").rows) =~ secret
    assert {:error, :invalid_request} = Accounts.login(owner, secret, "invented")
  end

  test "exact expiry revoked reopen and independent credentials", c do
    {owner, secret} = WorkHarbor.AuthDatabase.fixture!()
    {:ok, token, p} = Accounts.login(owner, secret, "cli_owner")
    {:ok, other, _} = Accounts.login(owner, secret, "cli_owner")
    Application.put_env(:workharbor, :clock, fn -> p.expires_at - 1 end)
    assert {:ok, _} = Accounts.authenticate(token)
    Application.put_env(:workharbor, :clock, fn -> p.expires_at end)
    assert {:error, :unauthenticated} = Accounts.authenticate(token)
    Application.put_env(:workharbor, :clock, fn -> p.issued_at end)
    assert :ok = Accounts.revoke(p, p.credential_id)
    GenServer.stop(c.pid)
    pid = WorkHarbor.AuthDatabase.open!(c.path)
    assert {:error, :unauthenticated} = Accounts.authenticate(token)
    assert {:ok, _} = Accounts.authenticate(other)
    GenServer.stop(pid)
  end

  test "read scope denies mutations grants unknown action and foreign ownership" do
    {owner, secret} = WorkHarbor.AuthDatabase.fixture!()
    {:ok, _, p} = Accounts.login(owner, secret, "dashboard")

    for a <- [:logout, :revoke, :issue_dashboard, :primary_login, :unknown],
        do: assert({:error, :forbidden} == Accounts.authorize(p, a, owner))

    assert {:error, :forbidden} = Accounts.revoke(p, p.credential_id)
    assert {:error, :forbidden} = Accounts.issue_dashboard(p)
    assert DB.count("owner_credentials") == 1
    assert DB.sql("SELECT revoked_at FROM owner_credentials").rows == [[nil]]

    assert DB.sql("SELECT count(*) FROM audit_events WHERE event='authorization_denied'").rows ==
             [[7]]

    {:ok, _, write} = Accounts.login(owner, secret, "cli_owner")
    assert {:error, :forbidden} = Accounts.authorize(write, :revoke, Ecto.UUID.generate())
  end

  test "read issuance and fresh login do not automatically revoke previous tokens" do
    {owner, secret} = WorkHarbor.AuthDatabase.fixture!()
    {:ok, first, p} = Accounts.login(owner, secret, "owner_browser")
    assert {:ok, read, r} = Accounts.issue_dashboard(p)
    assert r.scopes == ["dashboard:read"]
    assert {:ok, _, _} = Accounts.login(owner, secret, "owner_browser")
    assert {:ok, _} = Accounts.authenticate(first)
    assert {:ok, _} = Accounts.authenticate(read)
    assert :ok = Accounts.revoke(p, r.credential_id)
    assert {:error, :unauthenticated} = Accounts.authenticate(read)
  end

  test "revocation audit distinguishes actor target and unrelated viewer" do
    {owner, secret} = WorkHarbor.AuthDatabase.fixture!()
    {:ok, _, actor} = Accounts.login(owner, secret, "cli_owner")
    {:ok, first, target} = Accounts.issue_dashboard(actor)
    {:ok, second, _} = Accounts.issue_dashboard(actor)
    assert :ok = Accounts.revoke(actor, target.credential_id)

    [[metadata]] =
      DB.sql("SELECT metadata FROM audit_events WHERE event='credential_revoked'").rows

    assert Jason.decode!(metadata) == %{
             "actor_credential_id" => actor.credential_id,
             "target_credential_id" => target.credential_id
           }

    assert {:error, :unauthenticated} = Accounts.authenticate(first)
    assert {:ok, _} = Accounts.authenticate(second)
  end

  test "direct SQL cannot relabel credentials extend expiry or delete authenticator" do
    {owner, secret} = WorkHarbor.AuthDatabase.fixture!()
    {:ok, _, p} = Accounts.login(owner, secret, "dashboard")

    for sql <- [
          "UPDATE owner_credentials SET kind='cli_owner'",
          "UPDATE owner_credentials SET expires_at=expires_at+1",
          "UPDATE owner_authenticators SET owner_id='invented'",
          "DELETE FROM owner_authenticators"
        ] do
      assert_raise Exqlite.Error, fn -> DB.sql(sql) end
    end

    assert Repo.get(Accounts.Credential, p.credential_id).kind == "dashboard"
  end

  test "foreign or malformed revoke denial is audited without raw target content" do
    {owner, secret} = WorkHarbor.AuthDatabase.fixture!()
    {:ok, _, p} = Accounts.login(owner, secret, "cli_owner")
    assert {:error, :not_found} = Accounts.revoke(p, "SECRET-CANARY")

    [[action, metadata]] =
      DB.sql("SELECT action,metadata FROM audit_events WHERE event='authorization_denied'").rows

    assert action == "revoke"
    refute metadata =~ "SECRET-CANARY"
    assert Jason.decode!(metadata)["credential_id"] == p.credential_id
  end

  test "bootstrap preserves owner metadata audit and rejects nested transactions" do
    {owner, _} = WorkHarbor.AuthDatabase.fixture!()

    assert DB.sql("SELECT event FROM audit_events WHERE event='owner_created' AND owner_id=?", [
             owner
           ]).rows == [["owner_created"]]

    assert {:ok, :ok} =
             Repo.transaction(fn ->
               assert {:error, :nested_transaction} =
                        Accounts.bootstrap(fn _ -> flunk("nested delivery") end)

               :ok
             end)
  end

  test "existing foreign credential revoke is concealed audited and unchanged" do
    {owner, secret} = WorkHarbor.AuthDatabase.fixture!()
    {:ok, _, actor} = Accounts.login(owner, secret, "cli_owner")
    {:ok, foreign} = WorkHarbor.Foundation.create_owner("foreign metadata fixture")

    target =
      Repo.insert!(%Accounts.Credential{
        owner_id: foreign.id,
        kind: "dashboard",
        token_hash: :crypto.strong_rand_bytes(32),
        issued_at: Accounts.now(),
        expires_at: Accounts.now() + 100
      })

    assert Repo.get(Accounts.Credential, target.id) == target
    assert {:error, :not_found} = Accounts.revoke(actor, target.id)
    assert Repo.get(Accounts.Credential, target.id) == target

    [[audit_owner, action, code, metadata]] =
      DB.sql(
        "SELECT owner_id,action,code,metadata FROM audit_events WHERE event='authorization_denied'"
      ).rows

    assert audit_owner == owner and action == "revoke" and code == "not_found"
    assert Jason.decode!(metadata) == %{"credential_id" => actor.credential_id}
  end
end
