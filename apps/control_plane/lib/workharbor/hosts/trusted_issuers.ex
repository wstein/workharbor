# SPDX-License-Identifier: EUPL-1.2

defmodule WorkHarbor.Hosts.TrustedIssuers do
  @moduledoc """
  Client-CA set for the agent listener, following issuer rotation without a restart.

  Rotation replaces the issuer state file. The set is the current issuer plus every
  earlier issuer seen by this process that has not yet expired, so certificates issued
  before the rotation keep working for their remaining validity. A replacement that fails
  `Issuer.load/1` never widens or shrinks trust. The set is process-local: after a restart
  only the then-current issuer is known.
  """
  alias WorkHarbor.Hosts.Issuer

  def issuers(dir) when is_binary(dir) do
    now = WorkHarbor.Accounts.now()

    :global.trans({__MODULE__, dir}, fn ->
      state = refresh(dir, :persistent_term.get({__MODULE__, dir}, nil), now)
      :persistent_term.put({__MODULE__, dir}, state)
      state.issuers
    end)
  end

  defp refresh(dir, cached, now) do
    stamp = stamp(dir)
    cached = cached || %{stamp: nil, issuers: []}

    if stamp != nil and stamp == cached.stamp do
      %{cached | issuers: live(cached.issuers, now)}
    else
      case Issuer.load(dir) do
        {:ok, current} when stamp != nil ->
          previous = Enum.reject(cached.issuers, &(&1.certificate == current.certificate))
          %{stamp: stamp, issuers: live([current | previous], now)}

        _ ->
          %{cached | issuers: live(cached.issuers, now)}
      end
    end
  end

  defp live(issuers, now), do: Enum.filter(issuers, &(&1.expires_at > now))

  defp stamp(dir) do
    case File.stat(Path.join(dir, "issuer.json"), time: :posix) do
      {:ok, s} -> {s.inode, s.size, s.mtime, s.ctime}
      _ -> nil
    end
  end

  @doc "ssl verify_fun: the static root verifies normally; rotated issuers are checked here."
  def verify(cert, {:bad_cert, :unknown_ca} = event, dir) do
    if trusted?(cert, dir), do: {:valid, dir}, else: {:fail, event}
  end

  def verify(_cert, {:bad_cert, _} = event, _dir), do: {:fail, event}
  def verify(_cert, {:extension, _}, dir), do: {:unknown, dir}
  def verify(_cert, :valid, dir), do: {:valid, dir}
  def verify(_cert, :valid_peer, dir), do: {:valid, dir}

  defp trusted?(cert, dir) do
    der = :public_key.pkix_encode(:OTPCertificate, cert, :otp)
    info = Issuer.inspect_certificate(der)
    now = WorkHarbor.Accounts.now()

    Enum.any?(issuers(dir), fn issuer ->
      issuer.certificate == der or
        (info.issuer == issuer.subject and info.not_before <= now and now < info.not_after and
           now < issuer.expires_at and
           :public_key.pkix_verify(
             der,
             {{:ECPoint, issuer.public}, {:namedCurve, {1, 3, 101, 112}}}
           ))
    end)
  rescue
    _ -> false
  catch
    _ -> false
  end
end
