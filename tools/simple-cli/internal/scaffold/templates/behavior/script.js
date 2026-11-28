/**
 * Record Behavior: {{.TableName}}
 *
 * Events: load, update, submit
 * Reference: .simple/context/08-record-behaviors.md
 */

/**
 * A behavior is handed four inputs: `$form` (the Form API), `$db` (the
 * Database API), `$user` (the User Context) and `$ai` (the AI Context). This
 * starter takes only the one it uses. Add another to the list below when a
 * rule needs it.
 *
 * @param {object} context
 * @param {object} context.$form - The Form API
 */
export default async ({ $form }) => {
  // Handle 'load' event (Server + Client)
  if ($form.event === 'load') {
    // Logic for setting defaults, visibility, etc.
    // Example: $form('status').set('Draft');
  }

  // Handle 'update' event (Client Only)
  if ($form.event === 'update') {
    // Logic for reacting to field changes
    // Example: if ($form.updated('field')) { ... }
  }

  // Handle 'submit' event (Server + Client)
  if ($form.event === 'submit') {
    // Logic for validation
    // Example: if ($form('value').value() < 0) $form.error('Value must be positive');
  }
}
