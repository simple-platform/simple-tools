import Config

# Default mode is :cli (run the application logic)
config :scl_parser_cli, mode: :cli

# Import environment specific config
import_config "#{config_env()}.exs"
