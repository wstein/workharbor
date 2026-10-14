# SPDX-License-Identifier: EUPL-1.2

defmodule WorkHarbor.HostIdentityTest do
  use ExUnit.Case, async: false
  alias WorkHarbor.{Accounts, Hosts, HostFixture, Repo}
  alias WorkHarbor.Hosts.Issuer

  setup do
    dir = Path.join(HostFixture.physical_temp(), "whr-host-test-#{Ecto.UUID.generate()}")
    File.mkdir!(dir)
    File.chmod!(dir, 0o700)
    previous = Application.get_env(:workharbor, :host_identity)
    clock = Application.get_env(:workharbor, :clock)
    pid = WorkHarbor.HostDatabase.open!(Path.join(dir, "state.db"))
    {owner, secret} = WorkHarbor.AuthDatabase.fixture!()
    {:ok, token, _} = Accounts.login(owner, secret, "cli_owner")
    {:ok, principal} = Accounts.authenticate(token)
    issuer_dir = Path.join(dir, "issuer")

    on_exit(fn ->
      if previous,
        do: Application.put_env(:workharbor, :host_identity, previous),
        else: Application.delete_env(:workharbor, :host_identity)

      if clock,
        do: Application.put_env(:workharbor, :clock, clock),
        else: Application.delete_env(:workharbor, :clock)

      if Process.alive?(pid), do: GenServer.stop(pid)
      File.rm_rf!(dir)
    end)

    assert {:ok, _} = Issuer.initialize(issuer_dir)
    Application.put_env(:workharbor, :host_identity, issuer_dir: issuer_dir)
    %{dir: dir, issuer_dir: issuer_dir, principal: principal, repo: pid, secret: secret}
  end

  test "native issuer initializes exclusively and reloads same private identity", c do
    assert {:ok, first} = Issuer.load(c.issuer_dir)
    assert {:error, :already_initialized} = Issuer.initialize(c.issuer_dir)
    assert {:ok, second} = Issuer.load(c.issuer_dir)
    assert first.certificate == second.certificate
    assert Bitwise.band(File.stat!(Path.join(c.issuer_dir, "issuer.json")).mode, 0o777) == 0o600
  end

  test "issuer refuses unsafe missing future and malformed state without replacement", c do
    path = Path.join(c.issuer_dir, "issuer.json")
    File.chmod!(path, 0o644)
    assert {:error, :unsafe_identity} = Issuer.load(c.issuer_dir)
    File.chmod!(path, 0o600)
    File.write!(path, ~s({"schema_version":2,"private_key":"SECRET_SENTINEL"}))
    assert {:error, :invalid_identity} = Issuer.load(c.issuer_dir)
    assert {:error, :invalid_identity} = Issuer.load(Path.join(c.dir, "missing"))
  end

  test "single use enrollment derives host scope and code never persists", c do
    assert {:ok, issued} = Hosts.issue_enrollment(c.principal, nil)
    {csr, _} = HostFixture.csr()
    assert {:ok, enrolled} = Hosts.redeem(issued.code, csr)
    assert enrolled["host_id"] == issued.host_id
    assert enrolled["owner_id"] == c.principal.owner_id
    assert {:error, :invalid_enrollment} = Hosts.redeem(issued.code, csr)

    refute inspect(Repo.query!("SELECT * FROM host_enrollments", [], log: false).rows) =~
             issued.code

    refute inspect(Repo.all(WorkHarbor.Foundation.AuditEvent)) =~ issued.code
  end

  test "enrollment exact expiry issues no identities", c do
    now = Accounts.now()
    Application.put_env(:workharbor, :clock, fn -> now end)
    {:ok, issued} = Hosts.issue_enrollment(c.principal, nil)
    {csr, _} = HostFixture.csr()
    Application.put_env(:workharbor, :clock, fn -> now + 300 end)
    assert {:error, :invalid_enrollment} = Hosts.redeem(issued.code, csr)
    assert WorkHarbor.FoundationDatabase.count("host_identities") == 0
  end

  test "independent enrollment writers have exactly one committed winner", c do
    {:ok, issued} = Hosts.issue_enrollment(c.principal, nil)
    {csr, _} = HostFixture.csr()
    parent = self()

    tasks =
      for _ <- 1..2 do
        Task.async(fn ->
          Repo.put_dynamic_repo(c.repo)
          send(parent, {:ready, self()})

          receive do
            :go -> Hosts.redeem(issued.code, csr)
          end
        end)
      end

    for _ <- 1..2 do
      assert_receive {:ready, _}
    end

    Enum.each(tasks, &send(&1.pid, :go))
    results = Enum.map(tasks, &Task.await(&1, 5000))
    assert Enum.count(results, &match?({:ok, _}, &1)) == 1
    assert Enum.count(results, &(&1 == {:error, :invalid_enrollment})) == 1
    assert WorkHarbor.FoundationDatabase.count("host_identities") == 1
  end

  test "explicit host actions reject dashboard and cross owner before mutation", c do
    {:ok, token, _} = Accounts.login(c.principal.owner_id, c.secret, "dashboard")
    {:ok, dashboard} = Accounts.authenticate(token)
    assert {:error, :forbidden} = Hosts.issue_enrollment(dashboard, nil)
    {:ok, issued} = Hosts.issue_enrollment(c.principal, nil)
    {:ok, foreign} = WorkHarbor.Foundation.create_owner("other")
    {:ok, host} = WorkHarbor.Foundation.create_host(foreign.id)
    assert {:error, :not_found} = Hosts.issue_enrollment(c.principal, host.id)
    assert {:error, :not_found} = Hosts.revoke(c.principal, host.id)
    assert {:ok, _} = Hosts.status(c.principal, issued.host_id)
  end

  test "revocation tombstones host codes identities and repeated revoke", c do
    {:ok, issued} = Hosts.issue_enrollment(c.principal, nil)
    {csr, _} = HostFixture.csr()
    {:ok, enrolled} = Hosts.redeem(issued.code, csr)
    cert = Base.decode64!(enrolled["certificate"])
    assert {:ok, _} = Hosts.resolve_identity(cert, :active)
    assert :ok = Hosts.revoke(c.principal, issued.host_id)
    assert {:error, :unauthorized_host} = Hosts.resolve_identity(cert, :active)
    assert {:error, :host_revoked} = Hosts.issue_enrollment(c.principal, issued.host_id)
    assert :ok = Hosts.revoke(c.principal, issued.host_id)
    assert {:ok, %{revoked: true}} = Hosts.status(c.principal, issued.host_id)
  end

  test "staged rotation pending activation and lost ack preserve one identity", c do
    {:ok, issued} = Hosts.issue_enrollment(c.principal, nil)
    {csr, _} = HostFixture.csr()
    {:ok, enrolled} = Hosts.redeem(issued.code, csr)
    old = Base.decode64!(enrolled["certificate"])
    {:ok, peer} = Hosts.resolve_identity(old, :active)
    {renewal, _} = HostFixture.csr(issued.host_id)
    assert {:ok, pending} = Hosts.renew(peer, renewal)
    assert {:ok, ^pending} = Hosts.renew(peer, renewal)
    new = Base.decode64!(pending["certificate"])
    assert {:error, :unauthorized_host} = Hosts.resolve_identity(new, :active)
    {:ok, replacement} = Hosts.resolve_identity(new, :activation)
    assert :ok = Hosts.activate(replacement)
    assert :ok = Hosts.activate(replacement)
    assert {:error, :unauthorized_host} = Hosts.resolve_identity(old, :active)
    assert {:ok, _} = Hosts.resolve_identity(new, :active)
    assert :ok = Hosts.revoke(c.principal, issued.host_id)
    assert {:error, :unauthorized_host} = Hosts.activate(replacement)
  end

  test "CSR signature corruption and enrollment names cannot issue identity", c do
    {:ok, issued} = Hosts.issue_enrollment(c.principal, nil)
    {csr, _} = HostFixture.csr()
    size = byte_size(csr) - 1
    <<prefix::binary-size(^size), last>> = csr
    assert {:error, :invalid_csr} = Hosts.redeem(issued.code, prefix <> <<Bitwise.bxor(last, 1)>>)
    {named, _} = HostFixture.csr(issued.host_id)
    assert {:error, :invalid_csr} = Hosts.redeem(issued.code, named)
    assert WorkHarbor.FoundationDatabase.count("host_identities") == 0
  end

  test "certificate expiry and response remain identical across UTC tick redemption and renewal",
       c do
    {:ok, issued} = Hosts.issue_enrollment(c.principal, nil)
    {csr, _} = HostFixture.csr()
    base = Accounts.now()
    Process.put(:t008_tick, 0)

    Application.put_env(:workharbor, :clock, fn ->
      n = Process.get(:t008_tick, 0)
      Process.put(:t008_tick, n + 1)
      base + n
    end)

    {:ok, enrolled} = Hosts.redeem(issued.code, csr)
    cert = Base.decode64!(enrolled["certificate"])
    assert Issuer.inspect_certificate(cert).not_after == enrolled["expires_at"]
    {:ok, peer} = Hosts.resolve_identity(cert, :active)
    {renewal, _} = HostFixture.csr(issued.host_id)
    {:ok, pending} = Hosts.renew(peer, renewal)

    assert Issuer.inspect_certificate(Base.decode64!(pending["certificate"])).not_after ==
             pending["expires_at"]
  end

  test "revoked tombstone scope cannot move clear delete or admit reenrollment", c do
    {:ok, issued} = Hosts.issue_enrollment(c.principal, nil)
    :ok = Hosts.revoke(c.principal, issued.host_id)
    {:ok, other} = WorkHarbor.Foundation.create_host(c.principal.owner_id)

    for {statement, args} <- [
          {"UPDATE host_security SET host_id=? WHERE host_id=?", [other.id, issued.host_id]},
          {"UPDATE host_security SET revoked_at=NULL WHERE host_id=?", [issued.host_id]},
          {"DELETE FROM host_security WHERE host_id=?", [issued.host_id]}
        ] do
      assert_raise Exqlite.Error, fn -> Repo.query!(statement, args, log: false) end
    end

    assert {:error, :host_revoked} = Hosts.issue_enrollment(c.principal, issued.host_id)
  end

  test "CSR and private issuer DER suffixes reject before code consumption", c do
    {:ok, issued} = Hosts.issue_enrollment(c.principal, nil)
    {csr, _} = HostFixture.csr()

    for invalid <- [csr <> <<0>>, csr <> csr] do
      assert {:error, :invalid_csr} = Hosts.redeem(issued.code, invalid)
    end

    assert {:ok, _} = Hosts.redeem(issued.code, csr)
    path = Path.join(c.issuer_dir, "issuer.json")
    state = Jason.decode!(File.read!(path))

    for field <- ["certificate", "private_key"] do
      bad =
        Map.update!(state, field, fn value -> Base.encode64(Base.decode64!(value) <> <<0>>) end)

      File.write!(path, Jason.encode!(bad))
      assert {:error, :invalid_identity} = Issuer.load(c.issuer_dir)
    end
  end

  test "issuer refuses final and ancestor symlinks before initialization writes", c do
    real = Path.join(c.dir, "real")
    File.mkdir!(real)
    File.chmod!(real, 0o700)
    alias_path = Path.join(c.dir, "alias")
    File.ln_s!(real, alias_path)
    {:ok, _} = Issuer.initialize(Path.join(real, "issuer"))
    assert {:error, :unsafe_identity} = Issuer.load(Path.join(alias_path, "issuer"))
    assert {:error, :unsafe_identity} = Issuer.initialize(Path.join(alias_path, "fresh"))
    refute File.exists?(Path.join(real, "fresh"))
  end

  test "native leaf and issuer profiles reject extra purposes critical and duplicate extensions",
       c do
    {:ok, issuer} = Issuer.load(c.issuer_dir)
    {csr, _} = HostFixture.csr()
    host = Ecto.UUID.generate()
    {:ok, leaf} = Issuer.issue(issuer, csr, host, :enrollment)
    value = :public_key.pkix_decode_cert(leaf, :otp) |> elem(1)
    extensions = elem(value, 10)

    for changed <- [
          extensions ++ [{:Extension, {1, 2, 3, 4}, true, <<5, 0>>}],
          extensions ++ [{:Extension, {2, 5, 29, 37}, false, [{1, 3, 6, 1, 5, 5, 7, 3, 1}]}]
        ] do
      bad = :public_key.pkix_sign(put_elem(value, 10, changed), issuer.key)
      accepted = Issuer.valid_leaf?(bad, issuer, host, Accounts.now())
      refute accepted
    end

    ca = :public_key.pkix_decode_cert(issuer.certificate, :otp) |> elem(1)

    bad_ca =
      :public_key.pkix_sign(
        put_elem(
          ca,
          10,
          elem(ca, 10) ++ [{:Extension, {2, 5, 29, 37}, false, [{1, 3, 6, 1, 5, 5, 7, 3, 2}]}]
        ),
        issuer.key
      )

    accepted = Issuer.valid_leaf?(leaf, %{issuer | certificate: bad_ca}, host, Accounts.now())
    refute accepted
  end

  test "issuer startup rejects signed critical path length and duration profile variants", c do
    {:ok, issuer} = Issuer.load(c.issuer_dir)
    ca = :public_key.pkix_decode_cert(issuer.certificate, :otp) |> elem(1)
    path = Path.join(c.issuer_dir, "issuer.json")
    state = Jason.decode!(File.read!(path))

    variants = [
      elem(ca, 10) ++ [{:Extension, {1, 2, 3, 4}, true, <<5, 0>>}],
      Enum.map(elem(ca, 10), fn
        {:Extension, {2, 5, 29, 19}, critical, _} ->
          {:Extension, {2, 5, 29, 19}, critical, {:BasicConstraints, true, 0}}

        ext ->
          ext
      end)
    ]

    for extensions <- variants do
      signed = :public_key.pkix_sign(put_elem(ca, 10, extensions), issuer.key)
      File.write!(path, Jason.encode!(Map.put(state, "certificate", Base.encode64(signed))))
      result = Issuer.load(c.issuer_dir)
      rejected = result == {:error, :invalid_identity}
      assert rejected
    end

    File.write!(path, Jason.encode!(state))
    {csr, _} = HostFixture.csr()
    host = Ecto.UUID.generate()
    {:ok, leaf} = Issuer.issue(issuer, csr, host, :enrollment)
    tbs = :public_key.pkix_decode_cert(leaf, :otp) |> elem(1)
    {:Validity, start, _} = elem(tbs, 5)

    stop =
      {:generalTime,
       (Accounts.now() + 3600)
       |> DateTime.from_unix!()
       |> Calendar.strftime("%Y%m%d%H%M%SZ")
       |> String.to_charlist()}

    short = :public_key.pkix_sign(put_elem(tbs, 5, {:Validity, start, stop}), issuer.key)
    accepted = Issuer.valid_leaf?(short, issuer, host, Accounts.now())
    refute accepted
  end

  test "signed oversized issuer is refused before enrollment code consumption", c do
    {:ok, issuer} = Issuer.load(c.issuer_dir)
    {:ok, issued} = Hosts.issue_enrollment(c.principal, nil)
    path = Path.join(c.issuer_dir, "issuer.json")
    bytes = File.read!(path)
    original = Jason.decode!(bytes)
    ca = :public_key.pkix_decode_cert(issuer.certificate, :otp) |> elem(1)
    extensions = elem(ca, 10) ++ [{:Extension, {1, 2, 3, 4}, false, :binary.copy(<<0>>, 12000)}]
    oversized = :public_key.pkix_sign(put_elem(ca, 10, extensions), issuer.key)
    assert byte_size(oversized) > 4096
    File.write!(path, Jason.encode!(Map.put(original, "certificate", Base.encode64(oversized))))
    {csr, _} = HostFixture.csr()
    assert elem(Issuer.load(c.issuer_dir), 0) == :error
    refute Issuer.valid_issuer?(oversized)
    assert {:error, _} = Hosts.redeem(issued.code, csr)

    assert Hosts.one("SELECT consumed_at FROM host_enrollments WHERE host_id=?", [issued.host_id])[
             "consumed_at"
           ] == nil

    assert Hosts.one("SELECT count(*) AS n FROM host_identities WHERE host_id=?", [issued.host_id])[
             "n"
           ] == 0

    File.write!(path, bytes)
    assert {:ok, response} = Hosts.redeem(issued.code, csr)
    assert byte_size(Jason.encode!(response)) <= 16384
  end

  test "serialized owner revocation wins concurrent issuance redemption renewal and activation",
       c do
    for phase <- [:issue, :redeem, :renew, :activate] do
      {:ok, issued} = Hosts.issue_enrollment(c.principal, nil)
      {csr, _} = HostFixture.csr()

      peer =
        if phase in [:renew, :activate] do
          {:ok, enrolled} = Hosts.redeem(issued.code, csr)
          {:ok, active} = Hosts.resolve_identity(Base.decode64!(enrolled["certificate"]), :active)
          active
        end

      {renewal, _} = HostFixture.csr(issued.host_id)

      pending =
        if phase == :activate do
          {:ok, staged} = Hosts.renew(peer, renewal)

          {:ok, pending} =
            Hosts.resolve_identity(Base.decode64!(staged["certificate"]), :activation)

          pending
        end

      parent = self()

      job = fn fun ->
        Task.async(fn ->
          Repo.put_dynamic_repo(c.repo)
          send(parent, {:writer_ready, self()})

          receive do
            :write -> fun.()
          after
            2000 -> raise "writer barrier timeout"
          end
        end)
      end

      contender =
        job.(fn ->
          case phase do
            :issue -> Hosts.issue_enrollment(c.principal, issued.host_id)
            :redeem -> Hosts.redeem(issued.code, csr)
            :renew -> Hosts.renew(peer, renewal)
            :activate -> Hosts.activate(pending)
          end
        end)

      revoker = job.(fn -> Hosts.revoke(c.principal, issued.host_id) end)

      for _ <- 1..2 do
        assert_receive {:writer_ready, _}, 2000
      end

      send(contender.pid, :write)
      send(revoker.pid, :write)
      assert :ok = Task.await(revoker, 5000)
      result = Task.await(contender, 5000)
      assert result == :ok or elem(result, 0) in [:ok, :error]
      assert {:ok, %{revoked: true}} = Hosts.status(c.principal, issued.host_id)

      assert Hosts.one(
               "SELECT count(*) AS n FROM host_identities WHERE host_id=? AND status IN ('active','pending')",
               [issued.host_id]
             )["n"] == 0

      assert Hosts.one(
               "SELECT count(*) AS n FROM host_enrollments WHERE host_id=? AND revoked_at IS NULL",
               [issued.host_id]
             )["n"] == 0

      assert elem(Hosts.redeem(issued.code, csr), 0) == :error
      if peer, do: refute(Hosts.current_peer?(peer))
      if pending, do: refute(Hosts.current_peer?(pending, :activation))
    end
  end

  test "enrollment audit failure rolls back consumption identity and active fingerprint", c do
    {:ok, issued} = Hosts.issue_enrollment(c.principal, nil)
    {csr, _} = HostFixture.csr()

    Hosts.sql(
      "CREATE TRIGGER refuse_enrollment_audit BEFORE INSERT ON audit_events WHEN NEW.event='host_enrolled' BEGIN SELECT RAISE(ABORT,'SECRET_SENTINEL'); END"
    )

    assert {:error, :persistence_failed} = Hosts.redeem(issued.code, csr)

    assert Hosts.one("SELECT consumed_at FROM host_enrollments WHERE host_id=?", [issued.host_id])[
             "consumed_at"
           ] == nil

    assert Hosts.one("SELECT count(*) AS n FROM host_identities WHERE host_id=?", [issued.host_id])[
             "n"
           ] == 0

    assert Hosts.one("SELECT identity_fingerprint FROM hosts WHERE id=?", [issued.host_id])[
             "identity_fingerprint"
           ] == nil

    Hosts.sql("DROP TRIGGER refuse_enrollment_audit")
    assert {:ok, _} = Hosts.redeem(issued.code, csr)
  end

  test "native issuer persistence faults return fixed errors and restart only complete exclusive state",
       c do
    for stage <- [
          :file_open,
          :write,
          {:file, :sync},
          {:file, :close},
          :link,
          :parent_open,
          {:parent, :sync},
          {:parent, :close}
        ] do
      dir = Path.join(c.dir, "fault-#{Ecto.UUID.generate()}")
      Process.put({WorkHarbor.Hosts.PrivateFile, :test_io}, WorkHarbor.HostFileFault)
      Process.put({WorkHarbor.HostFileFault, :stage}, stage)

      try do
        assert {:error, :persistence_failed} = Issuer.initialize(dir)
      after
        Process.delete({WorkHarbor.Hosts.PrivateFile, :test_io})
        Process.delete({WorkHarbor.HostFileFault, :stage})
      end

      if stage in [:parent_open, {:parent, :sync}, {:parent, :close}] do
        assert {:ok, first} = Issuer.load(dir)
        assert {:error, :already_initialized} = Issuer.initialize(dir)
        assert {:ok, second} = Issuer.load(dir)
        assert first.certificate == second.certificate and first.key == second.key
      else
        refute File.exists?(Path.join(dir, "issuer.json"))
        assert {:ok, _} = Issuer.initialize(dir)
      end

      assert File.ls!(dir) == ["issuer.json"]
    end
  end
end
