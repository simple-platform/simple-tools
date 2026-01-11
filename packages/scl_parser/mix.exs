defmodule SCLParser.MixProject do
  use Mix.Project

  def project do
    [
      app: :scl_parser,
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
      description: "A line-aware SCL (Simple Configuration Language) parser.",
      package: package(),
      releases: releases()
    ]
  end

  # Run "mix help compile.app" to learn about applications.
  def application do
    [
      extra_applications: [:logger],
      mod: {SCLParser.Application, []}
    ]
  end

  def releases do
    [
      scl_parser: [
        steps: [:assemble, &Burrito.wrap/1, &rename_builds/1],
        burrito: [
          targets: get_targets(),
          debug: false
        ]
      ]
    ]
  end

  defp get_targets do
    targets = [
      macos: [os: :darwin, cpu: :aarch64],
      linux: [os: :linux, cpu: :x86_64],
      windows: [os: :windows, cpu: :x86_64]
    ]

    case System.get_env("BURRITO_TARGET") do
      nil -> targets
      target -> Keyword.take(targets, [String.to_atom(target)])
    end
  end

  defp package do
    [
      name: "scl_parser",
      organization: "simple",
      licenses: ["Apache-2.0"],
      files: ~w(lib .formatter.exs mix.exs README* LICENSE*),
      links: %{"GitHub" => "https://github.com/simple-dev/simple-tools"}
    ]
  end

  # Run "mix help deps" to learn about dependencies.
  defp deps do
    [
      {:credo, "1.7.15", only: [:dev, :test], runtime: false},
      {:excoveralls, "0.18.5", only: :test},
      {:burrito, "~> 1.0"}
    ]
  end

  defp rename_builds(release) do
    # Burrito output directory
    release_str = Atom.to_string(release.name)
    out_dir = Path.join("burrito_out", release_str) |> Path.dirname()

    # Rename output files to kebab-case
    File.ls!(out_dir)
    |> Enum.each(fn file ->
      if String.starts_with?(file, "scl_parser_") do
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
