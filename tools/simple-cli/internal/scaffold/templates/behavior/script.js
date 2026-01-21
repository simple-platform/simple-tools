/**
 * Record Behavior: {{.TableName}}
 *
 * Events: load, update, submit
 * Reference: .simple/context/08-record-behaviors.md
 */

/**
 * @param {object} context
 * @param {object} context.$form - The Form API
 * @param {object} context.$db - The Database API
 * @param {object} context.$user - The User Context
 * @param {object} context.$ai - The AI Context
 */
export default async ({ $ai, $db, $form, $user }) => {
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
