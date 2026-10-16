# SPDX-License-Identifier: EUPL-1.2

defmodule WorkHarbor.TrustedIssuersTest do
  use ExUnit.Case, async: false
  alias WorkHarbor.{HostFixture, Hosts}
  alias WorkHarbor.Hosts.{Issuer, TrustedIssuers}

  setup do
    base = Path.join(HostFixture.physical_temp(), "whr-trusted-#{Ecto.UUID.generate()}")
    File.mkdir!(base)
    File.chmod!(base, 0o700)
    dir = Path.join(base, "issuer")
    {:ok, first} = Issuer.initialize(dir)
    previous = Application.get_env(:workharbor, :host_identity)
    Application.put_env(:workharbor, :host_identity, issuer_dir: dir)

    on_exit(fn ->
      if previous,
        do: Application.put_env(:workharbor, :host_identity, previous),
        else: Application.delete_env(:workharbor, :host_identity)

      :persistent_term.erase({TrustedIssuers, dir})
      File.rm_rf!(base)
    end)

    %{base: base, dir: dir, first: first}
  end

  defp rotate(c) do
    fresh = Path.join(c.base, "fresh-#{Ecto.UUID.generate()}")
    {:ok, issuer} = Issuer.initialize(fresh)
    File.rename!(Path.join(fresh, "issuer.json"), Path.join(c.dir, "issuer.json"))
    issuer
  end

  defp certificates(dir), do: Enum.map(TrustedIssuers.issuers(dir), & &1.certificate)

  test "issuing remembers every issuer even when no handshake separates rotations", c do
    assert {:ok, _} = Hosts.issuer()
    second = rotate(c)
    assert {:ok, %{certificate: certificate}} = Hosts.issuer()
    assert certificate == second.certificate
    third = rotate(c)
    assert {:ok, _} = Hosts.issuer()

    assert certificates(c.dir) ==
             [third.certificate, second.certificate, c.first.certificate]
  end

  test "a failing issuer file keeps trust and is retried only after a backoff", c do
    assert certificates(c.dir) == [c.first.certificate]
    file = Path.join(c.dir, "issuer.json")
    File.write!(file, "{not json")
    assert certificates(c.dir) == [c.first.certificate]
    key = {TrustedIssuers, c.dir}
    %{failed: {_, until}} = :persistent_term.get(key)

    # Further attempts on the same bad file take neither the lock nor a reload.
    for _ <- 1..20, do: assert(certificates(c.dir) == [c.first.certificate])
    assert %{failed: {_, ^until}} = :persistent_term.get(key)

    File.rm!(file)
    assert certificates(c.dir) == [c.first.certificate]
    assert %{failed: {nil, _}} = :persistent_term.get(key)

    # A valid replacement is picked up at once, inside the backoff window.
    second = rotate(c)
    assert certificates(c.dir) == [second.certificate, c.first.certificate]
    assert %{failed: nil} = :persistent_term.get(key)
  end
end
