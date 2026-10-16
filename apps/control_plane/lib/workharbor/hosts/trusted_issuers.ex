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
    key = {__MODULE__, dir}
    cached = :persistent_term.get(key, nil)

    state =
      if cached != nil and cached.stamp != nil and cached.stamp == stamp(dir) do
        cached
      else
        # The lock only serialises refreshes; the hot path above takes none.
        :global.trans({key, self()}, fn ->
          latest = refresh(dir, :persistent_term.get(key, nil))
          :persistent_term.put(key, latest)
          latest
        end)
      end

    live(state.issuers, now)
  end

  defp refresh(dir, cached) do
    stamp = stamp(dir)
    cached = cached || %{stamp: nil, issuers: []}

    if stamp != nil and stamp == cached.stamp do
      cached
    else
      case Issuer.load(dir) do
        {:ok, current} when stamp != nil ->
          previous = Enum.reject(cached.issuers, &(&1.certificate == current.certificate))
          %{stamp: stamp, issuers: [current | previous]}

        _ ->
          # Unreadable, malformed, unsafe or invalid replacement: trust is unchanged.
          cached
      end
    end
  end

  defp live(issuers, now), do: Enum.filter(issuers, &(&1.expires_at > now))

  # Content digest, not mtime/inode: same-second replacements must not be missed.
  defp stamp(dir) do
    case File.read(Path.join(dir, "issuer.json")) do
      {:ok, bytes} when byte_size(bytes) <= 16_384 -> :crypto.hash(:sha256, bytes)
      _ -> nil
    end
  end

  @doc """
  ssl verify_fun. The static boot root verifies normally. For an unknown CA, ssl calls this
  with the TOP certificate of the chain and a valid result makes it the trust anchor, with no
  CA, key-usage or path-length checks. So the anchor is accepted only as either a set member
  (DER equality; the chain carries the issuer) or a complete end-entity leaf of a member, and
  the latter admits single-certificate chains only: every later event fails.
  """
  def verify(cert, {:bad_cert, :unknown_ca} = event, dir) when is_binary(dir) do
    case anchor(cert, dir) do
      nil -> {:fail, event}
      kind -> {:valid, {dir, kind}}
    end
  end

  def verify(_cert, {:bad_cert, _} = event, _state), do: {:fail, event}
  def verify(_cert, _event, {_dir, :leaf}), do: {:fail, :leaf_anchor_cannot_issue}
  def verify(_cert, {:extension, _}, state), do: {:unknown, state}
  def verify(_cert, :valid, state), do: {:valid, state}
  def verify(_cert, :valid_peer, state), do: {:valid, state}

  defp anchor(cert, dir) do
    der = :public_key.pkix_encode(:OTPCertificate, cert, :otp)
    now = WorkHarbor.Accounts.now()
    issuers = issuers(dir)

    cond do
      Enum.any?(issuers, &(&1.certificate == der)) -> :member
      Enum.any?(issuers, &Issuer.valid_leaf_profile?(der, &1, now)) -> :leaf
      true -> nil
    end
  rescue
    _ -> nil
  catch
    _ -> nil
  end
end
