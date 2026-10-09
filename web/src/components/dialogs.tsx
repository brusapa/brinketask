// Dialogs shared by the sidebar and the views: edit a list (name and
// colour), rename a tag, confirm a final action. React Aria's Modal traps
// focus, closes on Escape and gives the dialog its role and label.
import { useState } from "react";
import {
  Button,
  Dialog,
  Form,
  Heading,
  Input,
  Label,
  Modal,
  ModalOverlay,
  RadioButton,
  RadioField,
  RadioGroup,
  TextField,
} from "react-aria-components";
import { useTranslation } from "react-i18next";

import { listColors } from "./palette";

interface ModalProps {
  isOpen: boolean;
  onClose: () => void;
}

/** Name and colour of a list, for "New list" and "Edit list". */
export function ListDialog({
  isOpen,
  onClose,
  title,
  initialName,
  initialColor,
  onSave,
}: ModalProps & {
  title: string;
  initialName: string;
  initialColor: string | null;
  onSave: (name: string, color: string | null) => void;
}) {
  const { t } = useTranslation();
  const [name, setName] = useState(initialName);
  const [color, setColor] = useState(initialColor ?? "none");

  return (
    <ModalOverlay
      className="modal-overlay"
      isOpen={isOpen}
      onOpenChange={(open) => !open && onClose()}
      isDismissable
    >
      <Modal className="modal">
        <Dialog className="dialog">
          <Form
            onSubmit={(event) => {
              // A form submit would reload the page; the app handles it.
              event.preventDefault();
              const trimmed = name.trim();
              if (trimmed === "") return;
              onSave(trimmed, color === "none" ? null : color);
              onClose();
            }}
          >
            <Heading slot="title" className="dialog-title">
              {title}
            </Heading>
            <TextField
              className="field"
              value={name}
              onChange={setName}
              isRequired
              maxLength={200}
              autoFocus
            >
              <Label>{t("lists.name")}</Label>
              <Input className="input" />
            </TextField>
            <RadioGroup
              className="field"
              value={color}
              onChange={setColor}
              orientation="horizontal"
            >
              <Label>{t("lists.color")}</Label>
              <div className="swatches">
                <RadioField value="none" className="swatch" aria-label={t("colors.none")}>
                  <RadioButton className="swatch-button">
                    <span className="swatch-dot swatch-none" aria-hidden="true" />
                  </RadioButton>
                </RadioField>
                {listColors.map((c) => (
                  <RadioField
                    key={c.value}
                    value={c.value}
                    className="swatch"
                    aria-label={t(`colors.${c.key}`)}
                  >
                    <RadioButton className="swatch-button">
                      <span
                        className="swatch-dot"
                        style={{ backgroundColor: c.value }}
                        aria-hidden="true"
                      />
                    </RadioButton>
                  </RadioField>
                ))}
              </div>
            </RadioGroup>
            <div className="dialog-actions">
              <Button className="button" onPress={onClose}>
                {t("common.cancel")}
              </Button>
              <Button type="submit" className="button button-primary">
                {t("common.save")}
              </Button>
            </div>
          </Form>
        </Dialog>
      </Modal>
    </ModalOverlay>
  );
}

/** One text field: renaming a tag. */
export function NameDialog({
  isOpen,
  onClose,
  title,
  label,
  initialName,
  maxLength,
  onSave,
}: ModalProps & {
  title: string;
  label: string;
  initialName: string;
  maxLength: number;
  onSave: (name: string) => void;
}) {
  const { t } = useTranslation();
  const [name, setName] = useState(initialName);
  return (
    <ModalOverlay
      className="modal-overlay"
      isOpen={isOpen}
      onOpenChange={(open) => !open && onClose()}
      isDismissable
    >
      <Modal className="modal">
        <Dialog className="dialog">
          <Form
            onSubmit={(event) => {
              event.preventDefault();
              const trimmed = name.trim();
              if (trimmed === "") return;
              onSave(trimmed);
              onClose();
            }}
          >
            <Heading slot="title" className="dialog-title">
              {title}
            </Heading>
            <TextField
              className="field"
              value={name}
              onChange={setName}
              isRequired
              maxLength={maxLength}
              autoFocus
            >
              <Label>{label}</Label>
              <Input className="input" />
            </TextField>
            <div className="dialog-actions">
              <Button className="button" onPress={onClose}>
                {t("common.cancel")}
              </Button>
              <Button type="submit" className="button button-primary">
                {t("common.save")}
              </Button>
            </div>
          </Form>
        </Dialog>
      </Modal>
    </ModalOverlay>
  );
}

/** Asks before an action that cannot be undone. */
export function ConfirmDialog({
  isOpen,
  onClose,
  title,
  message,
  confirmLabel,
  onConfirm,
}: ModalProps & { title: string; message: string; confirmLabel: string; onConfirm: () => void }) {
  const { t } = useTranslation();
  return (
    <ModalOverlay
      className="modal-overlay"
      isOpen={isOpen}
      onOpenChange={(open) => !open && onClose()}
      isDismissable
    >
      <Modal className="modal">
        <Dialog className="dialog" role="alertdialog">
          <Heading slot="title" className="dialog-title">
            {title}
          </Heading>
          <p>{message}</p>
          <div className="dialog-actions">
            <Button className="button" onPress={onClose} autoFocus>
              {t("common.cancel")}
            </Button>
            <Button
              className="button button-danger"
              onPress={() => {
                onConfirm();
                onClose();
              }}
            >
              {confirmLabel}
            </Button>
          </div>
        </Dialog>
      </Modal>
    </ModalOverlay>
  );
}
