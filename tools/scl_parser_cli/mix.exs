defmodule SCLParserCLI.MixProject do
  use Mix.Project

  def project do
    [
      app: :scl_parser_cli,
      version: "1.0.0",
      elixir: "~> 1.18",
      start_permanent: Mix.env() == :prod,
      deps: deps(),
      test_coverage: [tool: ExCoveralls],
      preferred_cli_env: [
        coveralls: :test,
        "coveralls.detail": :test,
        "coveralls.post": :test,
        "coveralls.html": :test,
        "coveralls.cobertura": :test
      ],
      releases: releases()
    ]
  end

  def application do
    [
      extra_applications: [:logger],
      env: [mode: :cli],
      mod: {SCLParserCLI.Application, []}
    ]
  end

  def releases do
    [
      scl_parser_cli: [
        steps: [:assemble, &Burrito.wrap/1, &rename_builds/1],
        burrito: [
          targets: get_targets()
        ]
      ]
    ]
  end

  defp get_targets do
    targets = [
      macos: [os: :darwin, cpu: :x86_64],
      macos_silicon: [os: :darwin, cpu: :aarch64],
      linux: [os: :linux, cpu: :x86_64],
      linux_arm: [os: :linux, cpu: :aarch64],
      windows: [os: :windows, cpu: :x86_64]
    ]

    case System.get_env("BURRITO_TARGET") do
      nil -> targets
      target -> Keyword.take(targets, [String.to_atom(target)])
    end
  end

  defp deps do
    [
      {:scl_parser, "~> 1.0"},
      {:burrito, "~> 1.0"},
      {:credo, "1.7.15", only: [:dev, :test], runtime: false},
      {:excoveralls, "0.18.5", only: :test}
    ]
  end

  defp rename_builds(release) do
    release_str = Atom.to_string(release.name)
    out_dir = Path.join("burrito_out", release_str) |> Path.dirname()

    File.ls!(out_dir)
    |> Enum.each(fn file ->
      if String.contains?(file, "_") do
        new_name = String.replace(file, "_", "-")

        if new_name != file do
          Path.join(out_dir, file) |> File.rename(Path.join(out_dir, new_name))
          IO.puts("Renamed #{file} -> #{new_name}")
        end
      end
    end)

    release
  end
end
